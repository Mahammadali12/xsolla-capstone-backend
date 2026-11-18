package main

// @title           Xsolla School LMS API
// @description     API for Xsolla School LMS - Courses, Content Items, Submissions, and File Uploads
// @terms.url       https://xsolla.com/terms/
// @contact.name    Xsolla School LMS
// @contact.url     https://xsolla.com
// @contact.email   support@xsolla.com
// @license.name    MIT
// @license.url     https://opensource.org/licenses/MIT
// @version         1.0
// @host            localhost:8080
// @BasePath        /
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name Authorization
// @description Enter your JWT token as: Bearer <token>

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"xsolla-capstone-api/internal/config"
	"xsolla-capstone-api/internal/db"
	"xsolla-capstone-api/internal/handlers"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/outbox"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/storage"
	"xsolla-capstone-api/internal/store"

	_ "github.com/swaggo/files"
	"github.com/swaggo/http-swagger"
	_ "xsolla-capstone-api/docs"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer database.Close()
	if err := db.Migrate(database, cfg.MigrationsDir); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	// In-app notifications live in the notification service's database. Without NOTIFY_DATABASE_URL the
	// /notifications routes answer 503; nothing else needs it. The connection is lazy, so a database that
	// comes up after the API is used as soon as it answers.
	var notificationStore service.NotificationStore
	if cfg.NotifyDatabaseURL != "" {
		notifyDB, err := sql.Open("postgres", cfg.NotifyDatabaseURL)
		if err != nil {
			log.Fatalf("notify db: %v", err)
		}
		defer notifyDB.Close()
		if err := notifyDB.Ping(); err != nil {
			log.Printf("notify db: not reachable yet, /notifications tries again on each request: %v", err)
		}
		notificationStore = store.NewNotificationStore(notifyDB)
	} else {
		log.Print("notify db: NOTIFY_DATABASE_URL not set, /notifications answers 503")
	}

	store := store.NewStore(database)

	// Background publisher drains the transactional outbox written by bookings.
	// Runs until process exit; graceful shutdown is deferred to a later step.
	go outbox.NewPublisher(store, outbox.NewAMQPBroker(cfg.RabbitMQURL)).Run(context.Background())

	authSvc := service.NewAuthService(store, store)
	moduleSvc := service.NewModuleService(store)
	contentSvc := service.NewContentService(store)
	submissionSvc := service.NewSubmissionService(store)
	eventSvc := service.NewEventServiceWithGoogle(store, authSvc)
	userSvc := service.NewUserService(store)
	mentorSlotSvc := service.NewMentorSlotServiceWithGoogle(store, authSvc)
	teamSvc := service.NewTeamService(store, mentorSlotSvc)
	mentorReviewSvc := service.NewMentorReviewService(store)
	analyticsSvc := service.NewAnalyticsService(store)
	summaryProvider := service.NewGeminiSummaryProvider(cfg.GeminiAPIKey, cfg.GeminiModel,
		time.Duration(cfg.AISummaryTimeoutSeconds)*time.Second)
	analyticsSummarySvc := service.NewAnalyticsSummaryService(store, summaryProvider,
		cfg.AISummaryMaxInputChars, cfg.AISummaryMaxOutputChars)
	googleCalendarSvc := service.NewGoogleCalendarService(authSvc)

	// S3 object storage
	s3Storage, err := storage.NewS3Storage(cfg.S3Endpoint, cfg.S3Bucket, cfg.S3Region, cfg.S3AccessKey, cfg.S3SecretKey)
	if err != nil {
		log.Fatalf("s3 storage: %v", err)
	}
	fileSvc := service.NewFileService(store, s3Storage)
	aiQuizGen := service.NewGeminiQuizGenerator(cfg.GeminiAPIKey, cfg.GeminiModel)
	quizSvc := service.NewQuizService(store, fileSvc, aiQuizGen)
	profileSvc := service.NewProfileService(store, s3Storage)
	preferencesSvc := service.NewPreferencesService(store)

	authH := handlers.NewAuthHandlers(
		authSvc,
		cfg.JWTAccessSecret,
		cfg.JWTRefreshSecret,
		15*time.Minute,
		7*24*time.Hour,
	)
	moduleH := handlers.NewModuleHandlers(moduleSvc)
	contentH := handlers.NewContentHandlers(contentSvc)
	fileH := handlers.NewFileHandlers(fileSvc)
	submissionH := handlers.NewSubmissionHandlers(submissionSvc)
	eventH := handlers.NewEventHandlers(eventSvc)
	lectureFeedbackH := handlers.NewLectureFeedbackHandlers(service.NewLectureFeedbackService(store))
	reportH := handlers.NewReportHandlers(service.NewReportService(store))
	broadcastH := handlers.NewBroadcastHandlers(service.NewBroadcastService(store))
	notificationH := handlers.NewNotificationHandlers(service.NewNotificationService(notificationStore))
	userH := handlers.NewUserHandlers(userSvc)
	teamH := handlers.NewTeamHandlers(teamSvc)
	mentorSlotH := handlers.NewMentorSlotHandlers(mentorSlotSvc)
	mentorReviewH := handlers.NewMentorReviewHandlers(mentorReviewSvc)
	analyticsH := handlers.NewAnalyticsHandlers(analyticsSvc)
	analyticsSummaryH := handlers.NewAnalyticsSummaryHandlers(analyticsSummarySvc)
	quizH := handlers.NewQuizHandlers(quizSvc)
	// combined reads: many weeks (or tasks) in one request, with the same rules as the per-week routes
	batchH := handlers.NewBatchHandlers(contentSvc, quizSvc, moduleSvc, submissionSvc)
	googleCalendarH := handlers.NewGoogleCalendarHandlers(googleCalendarSvc)
	profileH := handlers.NewProfileHandlers(profileSvc, preferencesSvc, cfg.PublicAPIURL)
	// avatar_url on the people in user, team, event and 1:1 slot responses
	for _, h := range []interface {
		SetAvatars(handlers.AvatarSource, string)
	}{authH, userH, teamH, eventH, mentorSlotH} {
		h.SetAvatars(profileSvc, cfg.PublicAPIURL)
	}

	authenticator := &middleware.Authenticator{Secret: cfg.JWTAccessSecret}

	mux := http.NewServeMux()
	// Rate limits on sign-in and sign-up (AUTH_RATE_LIMIT=off turns them off, e.g. for test scripts):
	//   - sign-in: 20 at once per client address, then one every 3 seconds;
	//   - wrong passwords: 5 at once per account, then one a minute, from any address;
	//   - sign-up: 10 at once per client address, then one every 30 seconds.
	signupHandler, loginHandler := http.Handler(http.HandlerFunc(authH.Signup)), http.Handler(http.HandlerFunc(authH.Login))
	if cfg.AuthRateLimit {
		loginHandler = middleware.LimitByIP(middleware.NewLimiter(20, 3*time.Second), cfg.TrustProxyHeaders, "sign-in attempts",
			middleware.LimitFailedLogins(middleware.NewLimiter(5, time.Minute), loginHandler))
		signupHandler = middleware.LimitByIP(middleware.NewLimiter(10, 30*time.Second), cfg.TrustProxyHeaders, "sign-ups", signupHandler)
	} else {
		log.Print("auth: AUTH_RATE_LIMIT=off, sign-in and sign-up are not rate limited")
	}
	mux.Handle("POST /auth/signup", signupHandler)
	mux.Handle("POST /auth/login", loginHandler)
	mux.HandleFunc("POST /auth/refresh", authH.Refresh)
	mux.HandleFunc("POST /auth/logout", authH.Logout)
	mux.Handle("POST /auth/password-reset", authenticator.Middleware(http.HandlerFunc(authH.PasswordReset)))
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := database.PingContext(r.Context()); err != nil {
			log.Printf("readyz: database: %v", err)
			http.Error(w, "database not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.Handle("GET /auth/google/login", authenticator.OptionalAuth(http.HandlerFunc(authH.GoogleLogin)))
	mux.HandleFunc("GET /auth/google/callback", authH.GoogleCallback)
	mux.Handle("GET /auth/google/me", authenticator.Middleware(http.HandlerFunc(authH.GoogleMe)))
	mux.Handle("POST /auth/google/logout", authenticator.Middleware(http.HandlerFunc(authH.GoogleLogout)))
	mux.Handle("POST /auth/google/revoke", authenticator.Middleware(http.HandlerFunc(authH.GoogleRevoke)))
	mux.Handle("GET /google/calendar/events", authenticator.Middleware(http.HandlerFunc(googleCalendarH.ListEvents)))
	mux.Handle("GET /users/me", authenticator.Middleware(http.HandlerFunc(authH.Me)))

	// Admin-only user management (4.1)
	mux.Handle("GET /users", authenticator.RequireRole(http.HandlerFunc(userH.ListUsers), "admin", "lecturer", "mentor"))
	mux.Handle("POST /users", authenticator.RequireRole(http.HandlerFunc(userH.CreateUser), "admin"))
	mux.Handle("PATCH /users/{id}", authenticator.RequireRole(http.HandlerFunc(userH.UpdateUser), "admin"))
	mux.Handle("PUT /users/{id}/password", authenticator.RequireRole(http.HandlerFunc(userH.UpdatePassword), "admin"))
	mux.Handle("DELETE /users/{id}", authenticator.RequireRole(http.HandlerFunc(userH.DeleteUser), "admin"))
	// every non-admin user at once (optionally one role), with ?confirm=true
	mux.Handle("DELETE /users", authenticator.RequireRole(http.HandlerFunc(userH.DeleteAllUsers), "admin"))
	mux.Handle("PUT /users/{id}/roles", authenticator.RequireRole(http.HandlerFunc(userH.ReplaceUserRoles), "admin"))

	// Profile page: about me, picture, look and accessibility settings (every signed-in user)
	mux.Handle("GET /users/me/profile", authenticator.Middleware(http.HandlerFunc(profileH.GetMyProfile)))
	mux.Handle("PATCH /users/me/profile", authenticator.Middleware(http.HandlerFunc(profileH.UpdateMyProfile)))
	mux.Handle("GET /users/{id}/profile", authenticator.Middleware(http.HandlerFunc(profileH.GetProfile)))
	mux.Handle("PUT /users/me/avatar", authenticator.Middleware(http.HandlerFunc(profileH.PutMyAvatar)))
	mux.Handle("DELETE /users/me/avatar", authenticator.Middleware(http.HandlerFunc(profileH.DeleteMyAvatar)))
	mux.Handle("GET /users/me/preferences", authenticator.Middleware(http.HandlerFunc(profileH.GetMyPreferences)))
	mux.Handle("PATCH /users/me/preferences", authenticator.Middleware(http.HandlerFunc(profileH.UpdateMyPreferences)))
	// <img> tags cannot send a token, so pictures are public behind long random names
	mux.HandleFunc("GET /avatars/{name}", profileH.ServeAvatar)

	// Admin-only teams (4.2)
	mux.Handle("GET /teams", authenticator.RequireRole(http.HandlerFunc(teamH.ListTeams), "admin"))
	mux.Handle("PUT /teams", authenticator.RequireRole(http.HandlerFunc(teamH.ReplaceTeams), "admin"))
	mux.Handle("POST /teams", authenticator.RequireRole(http.HandlerFunc(teamH.CreateTeam), "admin"))
	mux.Handle("PATCH /teams/{id}", authenticator.RequireRole(http.HandlerFunc(teamH.PatchTeam), "admin"))
	mux.Handle("POST /teams/{id}/students", authenticator.RequireRole(http.HandlerFunc(teamH.AddTeamStudents), "admin"))
	mux.Handle("DELETE /teams/{id}", authenticator.RequireRole(http.HandlerFunc(teamH.DeleteTeam), "admin"))
	mux.Handle("DELETE /teams/{id}/students/{studentId}", authenticator.RequireRole(http.HandlerFunc(teamH.DeleteTeamStudent), "admin"))
	mux.Handle("DELETE /teams/{id}/students", authenticator.RequireRole(http.HandlerFunc(teamH.DeleteTeamStudents), "admin"))
	mux.Handle("PUT /teams/{id}/mentor", authenticator.RequireRole(http.HandlerFunc(teamH.PutTeamMentor), "admin"))

	// Team read side (student/mentor)
	mux.Handle("GET /teams/me", authenticator.Middleware(http.HandlerFunc(teamH.GetMyTeam)))
	mux.Handle("GET /teams/me/quiz-attempts", authenticator.Middleware(http.HandlerFunc(quizH.TeamQuizAttempts)))
	mux.Handle("GET /mentors/me/team", authenticator.Middleware(http.HandlerFunc(teamH.GetMentorTeam)))

	// Admin-only analytics (read-only reporting layer)
	mux.Handle("GET /analytics/overview", authenticator.RequireRole(http.HandlerFunc(analyticsH.Overview), "admin"))
	mux.Handle("GET /analytics/students", authenticator.RequireRole(http.HandlerFunc(analyticsH.ListStudents), "admin"))
	mux.Handle("GET /analytics/students/{studentId}", authenticator.RequireRole(http.HandlerFunc(analyticsH.Student), "admin"))
	mux.Handle("GET /analytics/mentors", authenticator.RequireRole(http.HandlerFunc(analyticsH.ListMentors), "admin"))
	mux.Handle("GET /analytics/mentors/{mentorId}", authenticator.RequireRole(http.HandlerFunc(analyticsH.Mentor), "admin"))
	mux.Handle("POST /analytics/students/{studentId}/ai-summary", authenticator.RequireRole(http.HandlerFunc(analyticsSummaryH.Student), "admin"))
	mux.Handle("POST /analytics/mentors/{mentorId}/ai-summary", authenticator.RequireRole(http.HandlerFunc(analyticsSummaryH.Mentor), "admin"))

	mux.Handle("POST /modules", authenticator.Middleware(http.HandlerFunc(moduleH.CreateModule)))
	mux.Handle("GET /modules", authenticator.Middleware(http.HandlerFunc(moduleH.ListModules)))
	mux.Handle("GET /modules/{id}", authenticator.Middleware(http.HandlerFunc(moduleH.GetModule)))
	mux.Handle("PATCH /modules/{id}", authenticator.Middleware(http.HandlerFunc(moduleH.PatchModule)))
	mux.Handle("DELETE /modules/{id}", authenticator.Middleware(http.HandlerFunc(moduleH.DeleteModule)))

	mux.Handle("POST /modules/{moduleId}/enrollments", authenticator.Middleware(http.HandlerFunc(moduleH.CreateEnrollment)))
	mux.Handle("DELETE /modules/{moduleId}/enrollments/{userId}", authenticator.Middleware(http.HandlerFunc(moduleH.DeleteEnrollment)))
	mux.Handle("GET /modules/{moduleId}/enrollments", authenticator.Middleware(http.HandlerFunc(moduleH.ListEnrollments)))
	mux.Handle("PATCH /modules/{moduleId}/enrollments/{userId}/grade", authenticator.Middleware(http.HandlerFunc(moduleH.GradeEnrollment)))

	mux.Handle("POST /modules/{moduleId}/content-items", authenticator.Middleware(http.HandlerFunc(contentH.CreateContentItem)))
	mux.Handle("GET /modules/{moduleId}/content-items", authenticator.Middleware(http.HandlerFunc(contentH.ListContentItems)))
	mux.Handle("GET /content-items/{id}", authenticator.Middleware(http.HandlerFunc(contentH.GetContentItem)))
	mux.Handle("PATCH /content-items/{id}", authenticator.Middleware(http.HandlerFunc(contentH.UpdateContentItem)))
	mux.Handle("DELETE /content-items/{id}", authenticator.Middleware(http.HandlerFunc(contentH.DeleteContentItem)))

	mux.Handle("POST /modules/{moduleId}/content-items/{assignmentId}/submissions", authenticator.Middleware(http.HandlerFunc(submissionH.CreateSubmission)))
	mux.Handle("GET /modules/{moduleId}/content-items/{assignmentId}/submissions", authenticator.Middleware(http.HandlerFunc(submissionH.ListSubmissions)))
	mux.Handle("GET /modules/{moduleId}/content-items/{assignmentId}/submissions/me", authenticator.Middleware(http.HandlerFunc(submissionH.GetMySubmission)))
	mux.Handle("POST /submissions/{id}/feedback", authenticator.Middleware(http.HandlerFunc(submissionH.SubmitFeedback)))
	mux.Handle("GET /submissions/{id}/files", authenticator.Middleware(http.HandlerFunc(submissionH.GetSubmissionFiles)))

	mux.Handle("PATCH /modules/{id}/grade", authenticator.Middleware(http.HandlerFunc(submissionH.GradeModule)))

	mux.Handle("GET /events", authenticator.Middleware(http.HandlerFunc(eventH.ListEvents)))
	// Lecture feedback: students fill the form after a lecture; only admins read the answers
	mux.Handle("GET /lecture-feedback/form", authenticator.Middleware(http.HandlerFunc(lectureFeedbackH.Form)))
	mux.Handle("GET /lecture-feedback/pending", authenticator.Middleware(http.HandlerFunc(lectureFeedbackH.Pending)))
	mux.Handle("GET /lecture-feedback/me", authenticator.Middleware(http.HandlerFunc(lectureFeedbackH.Mine)))
	mux.Handle("POST /lecture-feedback", authenticator.Middleware(http.HandlerFunc(lectureFeedbackH.Submit)))
	mux.Handle("GET /lecture-feedback", authenticator.RequireRole(http.HandlerFunc(lectureFeedbackH.List), "admin"))
	mux.Handle("GET /lecture-feedback/summary", authenticator.RequireRole(http.HandlerFunc(lectureFeedbackH.Summary), "admin"))

	// Reports: anyone signed in sends one; only admins read and handle them.
	mux.Handle("POST /reports", authenticator.Middleware(http.HandlerFunc(reportH.Send)))
	mux.Handle("GET /reports", authenticator.RequireRole(http.HandlerFunc(reportH.List), "admin"))
	mux.Handle("GET /reports/summary", authenticator.RequireRole(http.HandlerFunc(reportH.Summary), "admin"))
	mux.Handle("GET /reports/{id}", authenticator.RequireRole(http.HandlerFunc(reportH.Get), "admin"))
	mux.Handle("PATCH /reports/{id}", authenticator.RequireRole(http.HandlerFunc(reportH.Update), "admin"))
	mux.Handle("DELETE /reports/{id}", authenticator.RequireRole(http.HandlerFunc(reportH.Delete), "admin"))

	// In-app notifications: every signed-in user reads and marks only their own.
	mux.Handle("GET /notifications", authenticator.Middleware(http.HandlerFunc(notificationH.Unread)))
	mux.Handle("POST /notifications/read-all", authenticator.Middleware(http.HandlerFunc(notificationH.MarkAllRead)))
	mux.Handle("POST /notifications/{id}/read", authenticator.Middleware(http.HandlerFunc(notificationH.MarkRead)))
	// Announcements: the admin sends one to everyone or to one group; it arrives as an in-app notification and an email.
	mux.Handle("POST /notifications/broadcast", authenticator.RequireRole(http.HandlerFunc(broadcastH.Send), "admin"))
	mux.Handle("GET /notifications/broadcasts", authenticator.RequireRole(http.HandlerFunc(broadcastH.List), "admin"))
	mux.Handle("POST /events", authenticator.Middleware(http.HandlerFunc(eventH.CreateEvent)))
	mux.Handle("PATCH /events/{id}", authenticator.Middleware(http.HandlerFunc(eventH.UpdateEvent)))
	mux.Handle("DELETE /events/{id}", authenticator.Middleware(http.HandlerFunc(eventH.DeleteEvent)))

	// Mentor 1:1 slots
	mux.Handle("GET /mentor-slots", authenticator.Middleware(http.HandlerFunc(mentorSlotH.ListSlots)))
	mux.Handle("POST /mentor-slots", authenticator.Middleware(http.HandlerFunc(mentorSlotH.CreateAvailability)))
	mux.Handle("DELETE /mentor-slots/{id}", authenticator.Middleware(http.HandlerFunc(mentorSlotH.RemoveSlot)))
	mux.Handle("POST /mentor-slots/{id}/book", authenticator.Middleware(http.HandlerFunc(mentorSlotH.BookSlot)))
	mux.Handle("DELETE /mentor-slots/{id}/book", authenticator.Middleware(http.HandlerFunc(mentorSlotH.CancelBooking)))

	// Weekly mentor reviews (students only; the service answers 403 to everyone else)
	mux.Handle("GET /mentor-reviews/me", authenticator.Middleware(http.HandlerFunc(mentorReviewH.ListMine)))
	mux.Handle("POST /mentor-reviews", authenticator.Middleware(http.HandlerFunc(mentorReviewH.Submit)))
	// reading the answers: admins only (mentors never see the reviews about them)
	mux.Handle("GET /mentor-reviews", authenticator.RequireRole(http.HandlerFunc(mentorReviewH.List), "admin"))
	mux.Handle("GET /mentor-reviews/summary", authenticator.RequireRole(http.HandlerFunc(mentorReviewH.Summary), "admin"))
	mux.Handle("GET /mentor-reviews/missing", authenticator.RequireRole(http.HandlerFunc(mentorReviewH.Missing), "admin"))

	mux.Handle("POST /files", authenticator.Middleware(http.HandlerFunc(fileH.UploadFile)))
	mux.Handle("GET /files/{id}", authenticator.Middleware(http.HandlerFunc(fileH.GetFile)))
	mux.Handle("GET /files/{id}/download", authenticator.Middleware(http.HandlerFunc(fileH.DownloadFile)))
	mux.Handle("DELETE /files/{id}", authenticator.Middleware(http.HandlerFunc(fileH.DeleteFile)))
	mux.Handle("GET /modules/{moduleId}/quizzes", authenticator.Middleware(http.HandlerFunc(quizH.ListQuizzes)))

	// Combined reads (GET ...?module_ids=1,2,3 or ?assignment_ids=...): one request instead of one per week or task.
	mux.Handle("GET /content-items", authenticator.Middleware(http.HandlerFunc(batchH.ContentItems)))
	mux.Handle("GET /quizzes", authenticator.Middleware(http.HandlerFunc(batchH.Quizzes)))
	mux.Handle("GET /enrollments", authenticator.Middleware(http.HandlerFunc(batchH.Enrollments)))
	mux.Handle("GET /submissions/me", authenticator.Middleware(http.HandlerFunc(batchH.MySubmissions)))
	mux.Handle("POST /modules/{moduleId}/quizzes", authenticator.Middleware(http.HandlerFunc(quizH.CreateQuiz)))
	mux.Handle("POST /modules/{moduleId}/quizzes/ai", authenticator.Middleware(http.HandlerFunc(quizH.GenerateAIQuiz)))
	mux.Handle("GET /quizzes/{id}", authenticator.Middleware(http.HandlerFunc(quizH.GetQuiz)))
	mux.Handle("PATCH /quizzes/{id}", authenticator.Middleware(http.HandlerFunc(quizH.UpdateQuiz)))
	mux.Handle("DELETE /quizzes/{id}", authenticator.Middleware(http.HandlerFunc(quizH.DeleteQuiz)))
	mux.Handle("POST /quizzes/{id}/attempts", authenticator.Middleware(http.HandlerFunc(quizH.SubmitAttempt)))
	mux.Handle("/swagger/", httpSwagger.Handler(
		httpSwagger.URL("doc.json"),
	))

	addr := cfg.Port
	if addr[0] != ':' {
		addr = ":" + addr
	}
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, middleware.Logging(cors(mux, cfg.CORSOrigin))); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func cors(next http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//! w.Header().Set("Access-Control-Allow-Origin", origin)  TODO: temporarily disabled for CORS issue
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
