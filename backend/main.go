package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	adminHandler "healthlogin/backend/handler"
	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

func main() {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		getEnv("DB_HOST", "localhost"),
		getEnv("DB_PORT", "5432"),
		getEnv("DB_USER", "healthlogin"),
		getEnv("DB_PASSWORD", "healthlogin"),
		getEnv("DB_NAME", "healthlogin"),
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if err := waitForDB(db); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	jwtSecret := getEnv("JWT_SECRET", "dev-secret-change-me")

	// Repositories
	userRepo := repository.New(db)
	adminRepo := repository.NewAdminRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	tokenRepo := repository.NewTokenRepository(db)

	// Services
	authService := service.NewAuthService(userRepo)
	adminService := service.NewAdminService(userRepo, adminRepo, settingsRepo, tokenRepo, jwtSecret)

	// Middleware
	authMiddleware := middleware.NewAuthMiddleware(userRepo, adminService, jwtSecret)

	// Handlers
	h := NewHandler(authService)
	ah := adminHandler.NewAdminHandler(adminService)

	r := chi.NewRouter()
	r.Use(corsMiddleware)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Logger)

	// Public routes
	r.Get("/health", h.HealthHandler)
	r.Post("/register", h.RegisterHandler)
	r.Post("/login", h.LoginHandler)

	// Authenticated customer routes
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.RequireAuth)
		r.Post("/customer/finances/topup", ah.CreateTopUpRequestHandler)
		r.Get("/customer/profile", ah.GetProfileHandler)
		r.Post("/logout", ah.LogoutHandler)
	})

	// Authenticated admin routes
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.RequireAdmin)
		r.Get("/admin/users", ah.GetUsersHandler)
		r.Post("/admin/users/{id}/status", ah.UpdateUserStatusHandler)
		r.Get("/admin/finances/topups", ah.GetTopUpRequestsHandler)
		r.Post("/admin/finances/topups/{id}/approve", ah.ApproveTopUpRequestsHandler)
		r.Post("/admin/finances/topups/{id}/reject", ah.RejectTopUpRequestsHandler)
		r.Get("/admin/transactions", ah.GetTransactionsHandler)
		r.Get("/admin/settings", ah.GetSettingsHandler)
		r.Post("/admin/settings", ah.UpdateSettingsHandler)
	})

	addr := getEnv("HTTP_ADDR", ":8080")
	log.Printf("Starting server on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Could not start server: %v", err)
	}
}

// waitForDB retries db.Ping with a short backoff until the database is ready.
func waitForDB(db *sql.DB) error {
	var err error
	for i := 0; i < 10; i++ {
		err = db.Ping()
		if err == nil {
			return nil
		}
		log.Printf("Database not ready, retrying... (%d/10)", i+1)
		time.Sleep(2 * time.Second)
	}
	return err
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
