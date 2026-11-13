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
	"log"
	"net/http"
	"time"

	"xsolla-capstone-api/internal/config"
	"xsolla-capstone-api/internal/db"
	"xsolla-capstone-api/internal/handlers"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/storage"
	"xsolla-capstone-api/internal/store"

	"github.com/swaggo/http-swagger"
	_ "github.com/swaggo/files"
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

	store := store.NewStore(database)
	authSvc := service.NewAuthService(store, store)
	moduleSvc := service.NewModuleService(store)
	contentSvc := service.NewContentService(store)
	submissionSvc := service.NewSubmissionService(store)
	eventSvc := service.NewEventServiceWithGoogle(store, authSvc)
	userSvc := service.NewUserService(store)
	teamSvc := service.NewTeamService(store)
	mentorSlotSvc := service.NewMentorSlotService(store)

	// S3 object storage
	s3Storage, err := storage.NewS3Storage(cfg.S3Endpoint, cfg.S3Bucket, cfg.S3Region, cfg.S3AccessKey, cfg.S3SecretKey)
	if err != nil {
		log.Fatalf("s3 storage: %v", err)
	}
	fileSvc := service.NewFileService(store, s3Storage)
	aiQuizGen := service.NewGeminiQuizGenerator(cfg.GeminiAPIKey, cfg.GeminiModel)
	quizSvc := service.NewQuizService(store, fileSvc, aiQuizGen)

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
	userH := handlers.NewUserHandlers(userSvc)
	teamH := handlers.NewTeamHandlers(teamSvc)
	mentorSlotH := handlers.NewMentorSlotHandlers(mentorSlotSvc)
	quizH := handlers.NewQuizHandlers(quizSvc)

	authenticator := &middleware.Authenticator{Secret: cfg.JWTAccessSecret}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/signup", authH.Signup)
	mux.HandleFunc("POST /auth/login", authH.Login)
	mux.HandleFunc("POST /auth/refresh", authH.Refresh)
	mux.HandleFunc("POST /auth/logout", authH.Logout)
	mux.Handle("GET /auth/google/login", authenticator.OptionalAuth(http.HandlerFunc(authH.GoogleLogin)))
	mux.HandleFunc("GET /auth/google/callback", authH.GoogleCallback)
	mux.Handle("GET /auth/google/me", authenticator.Middleware(http.HandlerFunc(authH.GoogleMe)))
	mux.Handle("GET /users/me", authenticator.Middleware(http.HandlerFunc(authH.Me)))

	// Admin-only user management (4.1)
	mux.Handle("GET /users", authenticator.RequireRole(http.HandlerFunc(userH.ListUsers), "admin", "lecturer", "mentor"))
	mux.Handle("POST /users", authenticator.RequireRole(http.HandlerFunc(userH.CreateUser), "admin"))
	mux.Handle("PATCH /users/{id}", authenticator.RequireRole(http.HandlerFunc(userH.UpdateUser), "admin"))
	mux.Handle("PUT /users/{id}/password", authenticator.RequireRole(http.HandlerFunc(userH.UpdatePassword), "admin"))
	mux.Handle("DELETE /users/{id}", authenticator.RequireRole(http.HandlerFunc(userH.DeleteUser), "admin"))
	mux.Handle("PUT /users/{id}/roles", authenticator.RequireRole(http.HandlerFunc(userH.ReplaceUserRoles), "admin"))

	// Admin-only teams (4.2)
	mux.Handle("GET /teams", authenticator.RequireRole(http.HandlerFunc(teamH.ListTeams), "admin"))
	mux.Handle("PUT /teams", authenticator.RequireRole(http.HandlerFunc(teamH.ReplaceTeams), "admin"))
	mux.Handle("POST /teams", authenticator.RequireRole(http.HandlerFunc(teamH.CreateTeam), "admin"))
	mux.Handle("PATCH /teams/{id}", authenticator.RequireRole(http.HandlerFunc(teamH.PatchTeam), "admin"))
	mux.Handle("POST /teams/{id}/students", authenticator.RequireRole(http.HandlerFunc(teamH.AddTeamStudents), "admin"))
	mux.Handle("DELETE /teams/{id}/students/{studentId}", authenticator.RequireRole(http.HandlerFunc(teamH.DeleteTeamStudent), "admin"))
	mux.Handle("DELETE /teams/{id}/students", authenticator.RequireRole(http.HandlerFunc(teamH.DeleteTeamStudents), "admin"))
	mux.Handle("PUT /teams/{id}/mentor", authenticator.RequireRole(http.HandlerFunc(teamH.PutTeamMentor), "admin"))

	// Team read side (student/mentor)
	mux.Handle("GET /teams/me", authenticator.Middleware(http.HandlerFunc(teamH.GetMyTeam)))
	mux.Handle("GET /mentors/me/team", authenticator.Middleware(http.HandlerFunc(teamH.GetMentorTeam)))

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
	mux.Handle("POST /events", authenticator.Middleware(http.HandlerFunc(eventH.CreateEvent)))
	mux.Handle("PATCH /events/{id}", authenticator.Middleware(http.HandlerFunc(eventH.UpdateEvent)))
	mux.Handle("DELETE /events/{id}", authenticator.Middleware(http.HandlerFunc(eventH.DeleteEvent)))

	// Mentor 1:1 slots
	mux.Handle("GET /mentor-slots", authenticator.Middleware(http.HandlerFunc(mentorSlotH.ListSlots)))
	mux.Handle("POST /mentor-slots", authenticator.Middleware(http.HandlerFunc(mentorSlotH.CreateAvailability)))
	mux.Handle("DELETE /mentor-slots/{id}", authenticator.Middleware(http.HandlerFunc(mentorSlotH.RemoveSlot)))
	mux.Handle("POST /mentor-slots/{id}/book", authenticator.Middleware(http.HandlerFunc(mentorSlotH.BookSlot)))
	mux.Handle("DELETE /mentor-slots/{id}/book", authenticator.Middleware(http.HandlerFunc(mentorSlotH.CancelBooking)))

	mux.Handle("POST /files", authenticator.Middleware(http.HandlerFunc(fileH.UploadFile)))
	mux.Handle("GET /files/{id}", authenticator.Middleware(http.HandlerFunc(fileH.GetFile)))
	mux.Handle("GET /files/{id}/download", authenticator.Middleware(http.HandlerFunc(fileH.DownloadFile)))
	mux.Handle("DELETE /files/{id}", authenticator.Middleware(http.HandlerFunc(fileH.DeleteFile)))
	mux.Handle("GET /modules/{moduleId}/quizzes", authenticator.Middleware(http.HandlerFunc(quizH.ListQuizzes)))
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
