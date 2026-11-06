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
	moduleSvc := service.NewModuleService(store)
	contentSvc := service.NewContentService(store)
	fileSvc := service.NewFileService(store)
	submissionSvc := service.NewSubmissionService(store)
	eventSvc := service.NewEventService(store)

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

	authenticator := &middleware.Authenticator{Secret: cfg.JWTAccessSecret}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/signup", authH.Signup)
	mux.HandleFunc("POST /auth/login", authH.Login)
	mux.HandleFunc("POST /auth/refresh", authH.Refresh)
	mux.HandleFunc("POST /auth/logout", authH.Logout)
	mux.Handle("GET /users/me", authenticator.Middleware(http.HandlerFunc(authH.Me)))

	mux.Handle("POST /modules", authenticator.Middleware(http.HandlerFunc(moduleH.CreateModule)))
	mux.Handle("GET /modules", authenticator.Middleware(http.HandlerFunc(moduleH.ListModules)))
	mux.Handle("GET /modules/{id}", authenticator.Middleware(http.HandlerFunc(moduleH.GetModule)))
	mux.Handle("PATCH /modules/{id}", authenticator.Middleware(http.HandlerFunc(moduleH.PatchModule)))
	mux.Handle("DELETE /modules/{id}", authenticator.Middleware(http.HandlerFunc(moduleH.DeleteModule)))

	mux.Handle("POST /modules/{moduleId}/enrollments", authenticator.Middleware(http.HandlerFunc(moduleH.CreateEnrollment)))
	mux.Handle("DELETE /modules/{moduleId}/enrollments/{userId}", authenticator.Middleware(http.HandlerFunc(moduleH.DeleteEnrollment)))
	mux.Handle("GET /modules/{moduleId}/enrollments", authenticator.Middleware(http.HandlerFunc(moduleH.ListEnrollments)))

	mux.Handle("POST /modules/{moduleId}/content-items", authenticator.Middleware(http.HandlerFunc(contentH.CreateContentItem)))
	mux.Handle("GET /modules/{moduleId}/content-items", authenticator.Middleware(http.HandlerFunc(contentH.ListContentItems)))
	mux.Handle("GET /content-items/{id}", authenticator.Middleware(http.HandlerFunc(contentH.GetContentItem)))
	mux.Handle("PATCH /content-items/{id}", authenticator.Middleware(http.HandlerFunc(contentH.UpdateContentItem)))
	mux.Handle("DELETE /content-items/{id}", authenticator.Middleware(http.HandlerFunc(contentH.DeleteContentItem)))

	mux.Handle("POST /modules/{moduleId}/content-items/{assignmentId}/submissions", authenticator.Middleware(http.HandlerFunc(submissionH.CreateSubmission)))
	mux.Handle("GET /modules/{moduleId}/content-items/{assignmentId}/submissions", authenticator.Middleware(http.HandlerFunc(submissionH.ListSubmissions)))
	mux.Handle("GET /modules/{moduleId}/content-items/{assignmentId}/submissions/me", authenticator.Middleware(http.HandlerFunc(submissionH.GetMySubmission)))
	mux.Handle("POST /submissions/{id}/grade", authenticator.Middleware(http.HandlerFunc(submissionH.GradeSubmission)))
	mux.Handle("GET /submissions/{id}/files", authenticator.Middleware(http.HandlerFunc(submissionH.GetSubmissionFiles)))

	mux.Handle("GET /events", authenticator.Middleware(http.HandlerFunc(eventH.ListEvents)))
	mux.Handle("POST /events", authenticator.Middleware(http.HandlerFunc(eventH.CreateEvent)))
	mux.Handle("PATCH /events/{id}", authenticator.Middleware(http.HandlerFunc(eventH.UpdateEvent)))
	mux.Handle("DELETE /events/{id}", authenticator.Middleware(http.HandlerFunc(eventH.DeleteEvent)))

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
