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
	courseSvc := service.NewCourseService(store)
	contentSvc := service.NewContentService(store)
	fileSvc := service.NewFileService(store)
	submissionSvc := service.NewSubmissionService(store)

	authH := handlers.NewAuthHandlers(
		authSvc,
		cfg.JWTAccessSecret,
		cfg.JWTRefreshSecret,
		15*time.Minute,
		7*24*time.Hour,
	)
	courseH := handlers.NewCourseHandlers(courseSvc)
	contentH := handlers.NewContentHandlers(contentSvc)
	fileH := handlers.NewFileHandlers(fileSvc)
	submissionH := handlers.NewSubmissionHandlers(submissionSvc)

	authenticator := &middleware.Authenticator{Secret: cfg.JWTAccessSecret}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/signup", authH.Signup)
	mux.HandleFunc("POST /auth/login", authH.Login)
	mux.HandleFunc("POST /auth/refresh", authH.Refresh)
	mux.HandleFunc("POST /auth/logout", authH.Logout)
	mux.Handle("GET /users/me", authenticator.Middleware(http.HandlerFunc(authH.Me)))

	mux.Handle("POST /courses", authenticator.Middleware(http.HandlerFunc(courseH.CreateCourse)))
	mux.Handle("GET /courses", authenticator.Middleware(http.HandlerFunc(courseH.ListCourses)))
	mux.Handle("GET /courses/{id}", authenticator.Middleware(http.HandlerFunc(courseH.GetCourse)))
	mux.Handle("PATCH /courses/{id}", authenticator.Middleware(http.HandlerFunc(courseH.PatchCourse)))
	mux.Handle("DELETE /courses/{id}", authenticator.Middleware(http.HandlerFunc(courseH.DeleteCourse)))

	mux.Handle("POST /courses/{courseId}/enrollments", authenticator.Middleware(http.HandlerFunc(courseH.CreateEnrollment)))
	mux.Handle("DELETE /courses/{courseId}/enrollments/{userId}", authenticator.Middleware(http.HandlerFunc(courseH.DeleteEnrollment)))
	mux.Handle("GET /courses/{courseId}/enrollments", authenticator.Middleware(http.HandlerFunc(courseH.ListEnrollments)))

	mux.Handle("POST /courses/{courseId}/content-items", authenticator.Middleware(http.HandlerFunc(contentH.CreateContentItem)))
	mux.Handle("GET /courses/{courseId}/content-items", authenticator.Middleware(http.HandlerFunc(contentH.ListContentItems)))
	mux.Handle("GET /content-items/{id}", authenticator.Middleware(http.HandlerFunc(contentH.GetContentItem)))
	mux.Handle("PATCH /content-items/{id}", authenticator.Middleware(http.HandlerFunc(contentH.UpdateContentItem)))
	mux.Handle("DELETE /content-items/{id}", authenticator.Middleware(http.HandlerFunc(contentH.DeleteContentItem)))

	mux.Handle("POST /courses/{courseId}/content-items/{assignmentId}/submissions", authenticator.Middleware(http.HandlerFunc(submissionH.CreateSubmission)))
	mux.Handle("GET /courses/{courseId}/content-items/{assignmentId}/submissions", authenticator.Middleware(http.HandlerFunc(submissionH.ListSubmissions)))
	mux.Handle("GET /courses/{courseId}/content-items/{assignmentId}/submissions/me", authenticator.Middleware(http.HandlerFunc(submissionH.GetMySubmission)))
	mux.Handle("POST /submissions/{id}/grade", authenticator.Middleware(http.HandlerFunc(submissionH.GradeSubmission)))
	mux.Handle("GET /submissions/{id}/files", authenticator.Middleware(http.HandlerFunc(submissionH.GetSubmissionFiles)))

	mux.Handle("POST /files", authenticator.Middleware(http.HandlerFunc(fileH.UploadFile)))
	mux.Handle("GET /files/{id}", authenticator.Middleware(http.HandlerFunc(fileH.GetFile)))
	mux.Handle("GET /files/{id}/download", authenticator.Middleware(http.HandlerFunc(fileH.DownloadFile)))
	mux.Handle("DELETE /files/{id}", authenticator.Middleware(http.HandlerFunc(fileH.DeleteFile)))
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
