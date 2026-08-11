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

	_ "net/http/pprof"

	"healthlogin/backend/handler"
	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
	"healthlogin/backend/worker"
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
	orderRepo := repository.NewOrderRepository(db)
	shiftRepo := repository.NewShiftRepository(db)
	geozoneRepo := repository.NewGeozoneRepository(db)
	transactionRepo := repository.NewTransactionRepository(db)
	bidRepo := repository.NewBidRepository(db)
	chatRepo := repository.NewChatRepository(db)
	catalogRepo := repository.NewServiceCatalogRepository(db)
	appReleaseRepo := repository.NewAppReleaseRepository(db)

	// Services
	geocoder := service.NewGeocoder(db)
	authService := service.NewAuthService(userRepo, geocoder)
	adminService := service.NewAdminService(userRepo, adminRepo, settingsRepo, tokenRepo, jwtSecret)
	orderService := service.NewOrderService(orderRepo, transactionRepo, settingsRepo, userRepo, shiftRepo, chatRepo, catalogRepo, geocoder)
	shiftService := service.NewShiftService(shiftRepo, geozoneRepo, transactionRepo, settingsRepo, orderRepo, db)
	matchingService := service.NewMatchingService(orderRepo, shiftRepo, db)
	bidService := service.NewBidService(bidRepo, orderRepo, shiftRepo)
	chatService := service.NewChatService(chatRepo, orderRepo)

	// Start background order matcher
	matchingService.StartMatchingWorker(5 * time.Second)

	// Start background workers
	slaWorker := worker.NewSLAWorker(db, orderService, chatService)
	slaWorker.Start(30 * time.Second)

	auctionWorker := worker.NewAuctionWorker(db)
	auctionWorker.Start(1 * time.Minute)

	// Middleware
	authMiddleware := middleware.NewAuthMiddleware(userRepo, adminService, jwtSecret)

	// Handlers
	ph := handler.NewPublicHandler(authService)
	ah := handler.NewAdminHandler(adminService)
	oh := handler.NewOrderHandler(orderService)
	sh := handler.NewShiftHandler(shiftService)
	bh := handler.NewBidHandler(bidService, orderService)
	ch := handler.NewChatHandler(chatService)
	gh := handler.NewGeoHandler(geocoder)
	sch := handler.NewServiceCatalogHandler(catalogRepo)
	arh := handler.NewAppReleaseHandler(appReleaseRepo, getEnv("RELEASES_DIR", "releases"), getEnv("RELEASES_BASE_URL", ""))

	r := chi.NewRouter()
	r.Use(corsMiddleware)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Logger)

	// registerAPIRoutes wires every handler onto the given chi.Router. It is
	// mounted twice below so that BOTH /api/* (used by the web build via nginx
	// and by the rebuilt mobile app) and the legacy root paths /* (used by
	// already-installed mobile APKs that talk directly to port 8089) keep
	// working. The web nginx config only proxies /api/ to the backend, so root
	// paths are never exposed to browsers.
	registerAPIRoutes := func(r chi.Router) {
		r.Get("/health", ph.HealthHandler)
		r.Post("/register", ph.RegisterHandler)
		r.Post("/login", ph.LoginHandler)
		r.Get("/geo/geocode", gh.Geocode)
		r.Get("/geo/autocomplete", gh.Autocomplete)
		r.Get("/settings", ah.GetPublicSettingsHandler)
		r.Get("/service-categories", sch.ListRootCategories)
		r.Get("/service-categories/{id}/children", sch.ListChildren)
		r.Get("/service-categories/{id}/variants", sch.ListCategoryVariants)
		r.Get("/service-variants", sch.ListVariants)
		r.Get("/service-variants/{id}", sch.GetVariant)
		r.Get("/app/version", arh.GetVersionHandler)

		// Authenticated customer routes
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Post("/customer/finances/topup", ah.CreateTopUpRequestHandler)
			r.Get("/customer/profile", ah.GetProfileHandler)
			r.Post("/customer/orders", oh.CreateOrderHandler)
			r.Post("/customer/orders/construction", bh.CreateConstructionOrderHandler)
			r.Post("/customer/orders/{id}/confirm", oh.ConfirmOrderHandler)
			r.Post("/customer/orders/{id}/cancel", oh.CancelOrderHandler)
			r.Get("/customer/orders", oh.GetCustomerOrdersHandler)
			r.Post("/customer/bids/{id}/accept", bh.AcceptBidHandler)
			r.Get("/customer/orders/{id}/bids", bh.GetBidsHandler)
		})

		// Authenticated shared routes (customer + executor)
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Use(middleware.RequireRole("CUSTOMER", "EXECUTOR"))
			r.Get("/chats/{order_id}/messages", ch.GetMessagesHandler)
			r.Post("/chats/{order_id}/messages", ch.SendMessageHandler)
			r.Post("/chats/{order_id}/read", ch.MarkReadHandler)
			r.Get("/chats/unread-summary", ch.GetUnreadSummaryHandler)
			r.Get("/chats/{order_id}/ws", ch.WebSocketHandler)
			r.Post("/logout", ah.LogoutHandler)
		})

		// Authenticated executor routes
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Post("/executor/shifts", sh.StartShiftHandler)
			r.Post("/executor/shifts/end", sh.EndShiftHandler)
			r.Post("/executor/shifts/early-end", sh.EarlyEndShiftHandler)
			r.Post("/executor/shifts/location", sh.UploadLocationHandler)
			r.Get("/executor/shifts/active", sh.GetActiveShiftHandler)
			r.Get("/executor/orders/assigned", oh.GetExecutorAssignedOrdersHandler)
			r.Get("/executor/orders/available", bh.GetAvailableConstructionOrdersHandler)
			r.Get("/executor/orders/nearby", oh.GetNearbyOrdersHandler)
			r.Post("/executor/orders/{id}/accept", oh.AcceptOrder)
			r.Post("/executor/orders/{id}/bids", bh.CreateBidHandler)
		})

		// Authenticated admin routes
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Use(authMiddleware.RequireAdmin)
			r.Get("/admin/users", ah.GetUsersHandler)
			r.Post("/admin/users/{id}/status", ah.UpdateUserStatusHandler)
			r.Post("/admin/users/{id}/role", ah.UpdateUserRoleHandler)
			r.Post("/admin/users/{id}/address", ah.UpdateUserAddressHandler)
			r.Post("/admin/users/{id}/balance", ah.TopUpUserBalanceHandler)
			r.Get("/admin/finances/topups", ah.GetTopUpRequestsHandler)
			r.Post("/admin/finances/topups/{id}/approve", ah.ApproveTopUpRequestsHandler)
			r.Post("/admin/finances/topups/{id}/reject", ah.RejectTopUpRequestsHandler)
			r.Get("/admin/transactions", ah.GetTransactionsHandler)
			r.Get("/admin/settings", ah.GetSettingsHandler)
			r.Post("/admin/settings", ah.UpdateSettingsHandler)
			r.Get("/admin/shifts/active", ah.GetActiveShiftsHandler)
			r.Get("/admin/orders/active", ah.GetActiveOrdersHandler)
			r.Get("/admin/orders/completed", ah.GetCompletedOrdersHandler)
			r.Get("/admin/service-nodes", sch.AdminListNodes)
			r.Get("/admin/service-nodes/{id}", sch.AdminGetNode)
			r.Post("/admin/service-nodes", sch.AdminCreateNode)
			r.Put("/admin/service-nodes/{id}", sch.AdminUpdateNode)
			r.Delete("/admin/service-nodes/{id}", sch.AdminDeleteNode)
			r.Post("/admin/app-releases", arh.UploadReleaseHandler)
		})

		// Serve release files directly when not behind nginx.
		r.Get("/releases/*", http.StripPrefix("/releases/", http.FileServer(http.Dir("releases"))).ServeHTTP)
	}

	// Primary mount: /api/* (web via nginx + rebuilt mobile app).
	r.Route("/api", registerAPIRoutes)
	// Legacy mount: /* (already-installed mobile APKs talking directly to port
	// 8089). Kept until all clients are rebuilt with the /api interceptor.
	registerAPIRoutes(r)

	// Register pprof handlers for debugging (only exposed locally)
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	addr := getEnv("HTTP_ADDR", ":8080")
	certFile := getEnv("TLS_CERT_FILE", "")
	keyFile := getEnv("TLS_KEY_FILE", "")

	errChan := make(chan error, 2)

	if certFile != "" && keyFile != "" {
		go func() {
			log.Printf("Starting HTTPS server on %s", addr)
			errChan <- http.ListenAndServeTLS(addr, certFile, keyFile, r)
		}()
	} else {
		go func() {
			log.Printf("Starting HTTP server on %s", addr)
			errChan <- http.ListenAndServe(addr, r)
		}()
	}

	// Optional plain HTTP server for mobile/debug clients on the same network.
	// Set MOBILE_HTTP_ADDR (e.g. :8081) to enable. Disabled by default.
	if mobileAddr := getEnv("MOBILE_HTTP_ADDR", ""); mobileAddr != "" {
		go func() {
			log.Printf("Starting mobile HTTP server on %s", mobileAddr)
			errChan <- http.ListenAndServe(mobileAddr, r)
		}()
	}

	log.Fatalf("Server error: %v", <-errChan)
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

func buildAllowedOrigins() map[string]bool {
	origins := map[string]bool{
		"https://localhost":      true,
		"https://localhost:443":  true,
		"https://localhost:8443": true,
		"http://localhost":       true,
		"capacitor://localhost":  true,
		"ionic://localhost":      true,
	}
	if corsOrigin := getEnv("CORS_ORIGIN", ""); corsOrigin != "" {
		origins[corsOrigin] = true
	}
	return origins
}

func corsMiddleware(next http.Handler) http.Handler {
	allowedOrigins := buildAllowedOrigins()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
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
