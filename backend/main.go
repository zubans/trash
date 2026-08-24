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

	jwtSecret := getEnv("JWT_SECRET", "")
	if jwtSecret == "" {
		log.Fatalf("JWT_SECRET environment variable is required")
	}

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
	reviewRepo := repository.NewReviewRepository(db)
	executorGeoRepo := repository.NewExecutorGeoRepository(db)

	// Services
	geocoder := service.NewGeocoder(db)
	mailer := service.NewSmtpMailSender()
	authService := service.NewAuthServiceWithSecret(userRepo, jwtSecret, geocoder, mailer)
	adminService := service.NewAdminService(userRepo, adminRepo, settingsRepo, tokenRepo, jwtSecret, mailer)
	orderService := service.NewOrderService(orderRepo, transactionRepo, settingsRepo, userRepo, shiftRepo, chatRepo, catalogRepo, geocoder)
	shiftService := service.NewShiftService(shiftRepo, geozoneRepo, transactionRepo, settingsRepo, orderRepo, catalogRepo, db)
	matchingService := service.NewMatchingService(orderRepo, shiftRepo, db)
	bidService := service.NewBidService(bidRepo, orderRepo, shiftRepo, transactionRepo)
	chatService := service.NewChatService(chatRepo, orderRepo)
	reviewService := service.NewReviewService(reviewRepo, orderRepo)
	executorGeoService := service.NewExecutorGeoService(executorGeoRepo, orderRepo, geocoder)

	// Start background order matcher
	matchingService.StartMatchingWorker(5 * time.Second)

	// Start background workers
	slaWorker := worker.NewSLAWorker(db, orderService, chatService)
	slaWorker.Start(30 * time.Second)

	auctionWorker := worker.NewAuctionWorker(db)
	auctionWorker.Start(1 * time.Minute)

	shiftWorker := worker.NewShiftWorker(shiftService)
	shiftWorker.Start(1 * time.Minute)

	// Restore auto-end timers for existing active shifts on boot
	if activeShifts, err := shiftRepo.GetActiveShifts(); err == nil {
		for _, s := range activeShifts {
			shiftService.ScheduleShiftAutoEnd(s)
		}
	}

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
	rh := handler.NewReviewHandler(reviewService)
	egh := handler.NewExecutorGeoHandler(executorGeoService)

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
		r.Get("/auth/verify-email", ph.VerifyEmailHandler)
		r.Post("/auth/forgot-password", ph.ForgotPasswordHandler)
		r.Post("/auth/reset-password", ph.ResetPasswordHandler)
		r.Get("/geo/geocode", gh.Geocode)
		r.Get("/geo/autocomplete", gh.Autocomplete)
		r.Get("/settings", ah.GetPublicSettingsHandler)
		r.Get("/service-categories", sch.ListRootCategories)
		r.Get("/service-categories/{id}/children", sch.ListChildren)
		r.Get("/service-categories/{id}/variants", sch.ListCategoryVariants)
		r.Get("/service-variants", sch.ListVariants)
		r.Get("/service-variants/{id}", sch.GetVariant)
		r.Get("/app/version", arh.GetVersionHandler)
		r.Get("/users/{id}/reviews", rh.GetUserReviews)
		r.Get("/users/{id}/rating", rh.GetUserRating)

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

		// Authenticated shared routes (customer + executor + admin)
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Use(middleware.RequireRole("CUSTOMER", "EXECUTOR", "ADMIN"))
			r.Get("/auth/me", ph.MeHandler)
			r.Get("/user/profile", ah.GetProfileHandler)
			r.Post("/user/email", ph.UpdateEmailHandler)
			r.Post("/user/birth-date", ph.UpdateBirthDateHandler)
			r.Get("/chats/{order_id}/messages", ch.GetMessagesHandler)
			r.Post("/chats/{order_id}/messages", ch.SendMessageHandler)
			r.Put("/chats/{order_id}/messages/{message_id}", ch.EditMessageHandler)
			r.Delete("/chats/{order_id}/messages/{message_id}", ch.DeleteMessageHandler)
			r.Post("/chats/{order_id}/upload", ch.UploadAttachmentHandler)
			r.Post("/chats/{order_id}/read", ch.MarkReadHandler)
			r.Get("/chats/unread-summary", ch.GetUnreadSummaryHandler)
			r.Get("/chats/{order_id}/ws", ch.WebSocketHandler)
			r.Get("/support/chat", ch.GetUserSupportChatHandler)
			r.Get("/support/chats/{chat_id}/messages", ch.GetSupportMessagesHandler)
			r.Post("/support/chats/{chat_id}/messages", ch.SendSupportMessageHandler)
			r.Post("/support/chats/{chat_id}/upload", ch.UploadSupportAttachmentHandler)
			r.Post("/orders/{id}/reviews", rh.CreateReview)
			r.Get("/orders/{id}/reviews/mine", rh.GetOrderReview)
			r.Post("/finances/withdrawals", ah.CreateWithdrawalRequestHandler)
			r.Post("/logout", ah.LogoutHandler)
		})

		// Authenticated executor routes
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Post("/executor/shifts", sh.StartShiftHandler)
			r.Post("/executor/shifts/end", sh.EndShiftHandler)
			r.Post("/executor/shifts/early-end", sh.EarlyEndShiftHandler)
			r.Post("/executor/shifts/location", sh.UploadLocationHandler)
			r.Post("/executor/set-location", egh.SetLocation)
			r.Get("/executor/map-orders", egh.GetMapOrders)
			r.Get("/executor/shifts/active", sh.GetActiveShiftHandler)
			r.Get("/executor/history", sh.GetExecutorHistoryHandler)
			r.Get("/executor/orders/assigned", oh.GetExecutorAssignedOrdersHandler)
			r.Get("/executor/orders/available", bh.GetAvailableConstructionOrdersHandler)
			r.Get("/executor/orders/nearby", oh.GetNearbyOrdersHandler)
			r.Post("/executor/orders/{id}/accept", oh.AcceptOrder)
			r.Post("/executor/orders/{id}/execute", oh.ExecuteOrder)
			r.Post("/executor/orders/{id}/reject", oh.RejectOrderHandler)
			r.Post("/executor/orders/{id}/bids", bh.CreateBidHandler)
		})

		// Authenticated admin routes
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Use(authMiddleware.RequireAdmin)
			r.Get("/admin/geo-alerts", egh.GetGeoAlerts)
			r.Get("/admin/users", ah.GetUsersHandler)
			r.Post("/admin/users/{id}/status", ah.UpdateUserStatusHandler)
			r.Post("/admin/users/{id}/role", ah.UpdateUserRoleHandler)
			r.Post("/admin/users/{id}/address", ah.UpdateUserAddressHandler)
			r.Post("/admin/users/{id}/name", ah.UpdateUserNameHandler)
			r.Post("/admin/users/{id}/balance", ah.TopUpUserBalanceHandler)
			r.Get("/admin/finances/topups", ah.GetTopUpRequestsHandler)
			r.Post("/admin/finances/topups/{id}/approve", ah.ApproveTopUpRequestsHandler)
			r.Post("/admin/finances/topups/{id}/reject", ah.RejectTopUpRequestsHandler)
			r.Get("/admin/finances/withdrawals", ah.GetWithdrawalRequestsHandler)
			r.Post("/admin/finances/withdrawals/{id}/approve", ah.ApproveWithdrawalRequestsHandler)
			r.Post("/admin/finances/withdrawals/{id}/reject", ah.RejectWithdrawalRequestsHandler)
			r.Get("/admin/transactions", ah.GetTransactionsHandler)
			r.Get("/admin/settings", ah.GetSettingsHandler)
			r.Post("/admin/settings", ah.UpdateSettingsHandler)
			r.Get("/admin/support/chats", ch.GetAdminSupportChatListHandler)
			r.Get("/admin/support/unread-summary", ch.GetAdminSupportUnreadSummaryHandler)
			r.Post("/admin/support/chats/{chat_id}/ban", ch.BanSupportChatHandler)
			r.Post("/admin/support/chats/{chat_id}/unban", ch.UnbanSupportChatHandler)
			r.Get("/admin/shifts/active", ah.GetActiveShiftsHandler)
			r.Get("/admin/orders/active", ah.GetActiveOrdersHandler)
			r.Get("/admin/orders/completed", ah.GetCompletedOrdersHandler)
			r.Get("/admin/service-nodes", sch.AdminListNodes)
			r.Get("/admin/service-nodes/{id}", sch.AdminGetNode)
			r.Post("/admin/service-nodes", sch.AdminCreateNode)
			r.Put("/admin/service-nodes/{id}", sch.AdminUpdateNode)
			r.Delete("/admin/service-nodes/{id}", sch.AdminDeleteNode)
			r.Post("/admin/app-releases", arh.UploadReleaseHandler)
			r.Post("/admin/broadcast-email", ah.SendBroadcastEmailHandler)
		})
	}

	// Primary mount: /api/* (web via nginx + rebuilt mobile app).
	r.Route("/api", registerAPIRoutes)
	// Legacy mount: /* (already-installed mobile APKs talking directly to port
	// 8089). Kept until all clients are rebuilt with the /api interceptor.
	registerAPIRoutes(r)

	// Serve uploaded files and release APKs
	uploadsDir := getEnv("UPLOADS_DIR", "uploads")
	r.Get("/releases/*", http.StripPrefix("/releases/", http.FileServer(http.Dir(getEnv("RELEASES_DIR", "releases")))).ServeHTTP)
	r.Get("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadsDir))).ServeHTTP)
	r.Get("/api/uploads/*", http.StripPrefix("/api/uploads/", http.FileServer(http.Dir(uploadsDir))).ServeHTTP)

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
