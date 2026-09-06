package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	_ "net/http/pprof"

	"healthlogin/backend/achievement"
	"healthlogin/backend/achievements"
	"healthlogin/backend/behavior"
	"healthlogin/backend/behaviors"
	"healthlogin/backend/handler"
	"healthlogin/backend/metrics"
	"healthlogin/backend/middleware"
	"healthlogin/backend/money"
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
	configurePool(db)

	if err := waitForDB(db); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Наблюдаемость. Счётчики пула регистрируются до того, как им начнут
	// пользоваться, чтобы забитый пул был виден и на старте.
	metrics.RegisterDB(db, "main")
	metrics.SetBuildInfo(getEnv("APP_VERSION", "dev"), getEnv("GIT_COMMIT", "unknown"))

	jwtSecret := getEnv("JWT_SECRET", "")
	if jwtSecret == "" {
		log.Fatalf("JWT_SECRET environment variable is required")
	}

	// Сначала схема: процесс не должен стартовать на недостроенной схеме.
	// Ставьте SKIP_MIGRATIONS=1, когда миграции применяются отдельным шагом.
	if getEnv("SKIP_MIGRATIONS", "") == "" {
		if err := repository.Migrate(db, getEnv("MIGRATIONS_DIR", "migrations")); err != nil {
			log.Fatalf("Failed to apply migrations: %v", err)
		}
	}

	// Репозитории
	userRepo := repository.New(db)
	adminRepo := repository.NewAdminRepository(db)
	// system_settings — несколько строк, читаемых на путях ценообразования,
	// допуска и подбора, по нескольку раз за запрос и внутри циклов воркеров. Кэш
	// сквозной, поэтому правка админа всё равно применится к следующему заказу;
	// TTL лишь ограничивает устаревание от записей, сделанных не этим процессом.
	settingsRepo := repository.NewCachedSettingsRepository(
		repository.NewSettingsRepository(db),
		time.Duration(getEnvInt("SETTINGS_CACHE_TTL_SEC", 10))*time.Second,
	)
	tokenRepo := repository.NewTokenRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	shiftRepo := repository.NewShiftRepository(db)
	transactionRepo := repository.NewTransactionRepository(db)
	bidRepo := repository.NewBidRepository(db)
	chatRepo := repository.NewChatRepository(db)
	// Каждый заказ в списке разрешает свой вариант услуги, а предикат допуска
	// читает флаги этого варианта для каждого оцениваемого заказа. Сам каталог
	// меняется, только когда его правит админ, и эти правки идут через тот же
	// репозиторий и сбрасывают кэш.
	catalogRepo := repository.NewCachedServiceCatalogRepository(
		repository.NewServiceCatalogRepository(db),
		time.Duration(getEnvInt("CATALOG_CACHE_TTL_SEC", 60))*time.Second,
	)
	appReleaseRepo := repository.NewAppReleaseRepository(db)
	reviewRepo := repository.NewReviewRepository(db)
	refreshRepo := repository.NewRefreshTokenRepository(db)
	executorGeoRepo := repository.NewExecutorGeoRepository(db)
	addressRepo := repository.NewAddressRepository(db)
	reconcileRepo := repository.NewReconciliationRepository(db)
	systemAccountRepo := repository.NewSystemAccountRepository(db)
	// Скриптовые услуги: outbox, который читает диспетчер поведений, и claim'ы,
	// делающие услугу «один раз на пользователя» действительно однократной.
	eventRepo := repository.NewEventRepository(db)
	serviceClaimRepo := repository.NewServiceClaimRepository(db)
	// Данные, отправляемые исполнителем на проверку, и случаи, которые поведение
	// передаёт администратору при несовпадении.
	submissionRepo := repository.NewSubmissionRepository(db)
	// Геймификация: каталог ачивок и выдачи, агрегаты исполнителя, подарки,
	// внутренняя почта. Денежные инциденты живут рядом с ними, но существуют
	// сами по себе: они охраняют распределение заказа и нужны, даже когда ни
	// одна ачивка не включена.
	achievementRepo := repository.NewAchievementRepository(db)
	executorStatsRepo := repository.NewExecutorStatsRepository(db)
	giftRepo := repository.NewGiftRepository(db)
	mailRepo := repository.NewMailRepository(db)
	incidentRepo := repository.NewMoneyIncidentRepository(db)

	// Сервисы
	// Любое движение денег идёт через реестр, который всегда затрагивает и баланс
	// пользователя, и системный счёт.
	ledger := service.NewLedger(transactionRepo, systemAccountRepo).
		WithIncidents(incidentRepo)

	// Скрипты поведений несут правила услуг, чьи условия не укладываются во флаги
	// каталога (см. doc/service_behaviors.md). Первыми загружаются копии,
	// встроенные в бинарник; каталог поверх них позволяет поправить правило на
	// работающем деплое без пересборки. Скрипт, который не скомпилировался,
	// логируется и пропускается — узлы, называющие его, тогда отказывают в
	// безопасную сторону, и делают это громко.
	behaviorEngine := behavior.New(behavior.DefaultLimits)
	if err := behaviorEngine.Load(behaviors.FS, "embedded"); err != nil {
		log.Printf("[behavior] WARNING: %v", err)
	}
	if dir := getEnv("BEHAVIORS_DIR", ""); dir != "" {
		if err := behaviorEngine.Load(os.DirFS(dir), dir); err != nil {
			log.Printf("[behavior] WARNING: %v", err)
		}
	}
	serviceBehaviors := service.NewBehaviors(behaviorEngine, serviceClaimRepo).
		WithCatalog(catalogRepo)
	// Особые услуги несут собственный скрипт, написанный в админ-панели. Они
	// компилируются до первого запроса: узел, чей скрипт не загружен, закрывает
	// свои проверки в безопасную сторону, и сделать это при старте тише, чем
	// сделать это заказчику.
	if err := serviceBehaviors.SyncAll(context.Background()); err != nil {
		log.Printf("[behavior] WARNING: %v", err)
	}

	// Ачивки читаются тем же способом и с теми же оговорками, что и поведения:
	// сперва копии, встроенные в бинарник, затем каталог поверх них — чтобы
	// правило можно было поправить на работающем деплое без пересборки.
	achievementEngine := achievement.New(achievement.DefaultLimits)
	if err := achievementEngine.Load(achievements.FS, "embedded"); err != nil {
		log.Printf("[achievement] WARNING: %v", err)
	}
	if dir := getEnv("ACHIEVEMENTS_DIR", ""); dir != "" {
		if err := achievementEngine.Load(os.DirFS(dir), dir); err != nil {
			log.Printf("[achievement] WARNING: %v", err)
		}
	}
	// Собственные ачивки, написанные в админ-панели, компилируются из базы —
	// при старте, чтобы не ждать первого события, и дальше по таймеру, чтобы
	// правка на другой реплике дошла и сюда.
	achievementScripts := service.NewAchievements(achievementEngine, achievementRepo)
	if err := achievementScripts.SyncAll(context.Background()); err != nil {
		log.Printf("[achievement] WARNING: %v", err)
	}
	// Уровни — единственное место, где баллы превращаются в ставку комиссии.
	levels := service.NewLevels(achievementRepo, settingsRepo)

	// DaData — единственный источник адресных данных: и подсказок, и разрешения
	// координат. Запасного варианта намеренно нет: у альтернативы не было данных о
	// квартирах и она отвергала обычные номера домов, а молчаливое возвращение к
	// ней спрятало бы ошибку конфигурации за наполовину работающим вводом адреса.
	// Кэш избавляет провайдера от повторных разрешений одного и того же адреса на
	// запасном пути.
	addressSuggester := service.NewAddressSuggester(service.NewDaData(), repository.NewGeocodeCacheRepository(db))
	if addressSuggester.Configured() {
		log.Printf("[address] suggestions served by DaData")
	} else {
		// Не фатально: отсутствующий ключ не должен утаскивать за собой заказы, чат и
		// платежи. Ввод адреса отдаёт 503, пока ключ не задан.
		log.Printf("[address] WARNING: DADATA_API_KEY is not set — address suggestions will return 503 and registration cannot complete")
	}
	mailer := service.NewSmtpMailSender()
	// AuthService владеет всем, что связано с сессиями: выдачей access-токенов,
	// ротацией refresh-токенов и занесением отозванных access-токенов в чёрный список.
	authService := service.NewAuthServiceWithSecret(userRepo, jwtSecret, addressSuggester, mailer).
		WithAddresses(addressRepo).
		WithExecutorGeo(executorGeoRepo).
		WithSessionStorage(refreshRepo, tokenRepo)
	adminService := service.NewAdminService(userRepo, adminRepo, settingsRepo, jwtSecret, mailer).
		WithSessions(authService).
		WithLedger(ledger).
		WithAddresses(addressRepo).
		WithReconciliation(reconcileRepo).
		WithEvents(eventRepo)
	orderService := service.NewOrderService(orderRepo, ledger, settingsRepo, userRepo, shiftRepo, chatRepo, catalogRepo, addressSuggester).
		WithExecutorGeo(executorGeoRepo).
		WithBehaviors(serviceBehaviors, serviceClaimRepo, eventRepo).
		WithAchievements(levels, executorStatsRepo)
	executorGeoService := service.NewExecutorGeoService(executorGeoRepo, orderRepo).
		WithEligibility(userRepo, settingsRepo, catalogRepo).
		WithBehaviors(serviceBehaviors)
	// Отчёты о местоположении в смене пишутся через гео-сервис, поэтому у
	// сохранённой позиции исполнителя один писатель и один набор правил.
	shiftService := service.NewShiftService(shiftRepo, ledger, settingsRepo, orderRepo, catalogRepo, db).
		WithExecutorLocation(executorGeoService)
	// Автоматический подбор ограничен расстоянием, для чего нужны сохранённая
	// позиция исполнителя и настроенный радиус.
	matchingService := service.NewMatchingService(orderRepo, shiftRepo, userRepo, catalogRepo).
		WithGeo(executorGeoRepo, settingsRepo).
		WithBehaviors(serviceBehaviors)
	bidService := service.NewBidService(bidRepo, orderRepo, shiftRepo, ledger, userRepo, catalogRepo, chatRepo).
		WithBehaviors(serviceBehaviors, eventRepo)
	chatService := service.NewChatService(chatRepo, orderRepo)
	reviewService := service.NewReviewService(reviewRepo, orderRepo).
		WithExecutorStats(executorStatsRepo)

	// Каждая периодическая задача ниже меняет состояние, которое должно измениться
	// один раз: возврат, штраф, назначение. Защита лидером заставляет каждый тик
	// выполняться на одном процессе, поэтому вторая реплика его пропускает, а не
	// делает работу дважды. С одним процессом это стоит одной advisory-блокировки на тик и больше ничего.
	leader := worker.NewLeader(db)

	// Запускаем фоновый подборщик заказов
	matchingService.WithLeaderGuard(leader.Guard("matching"))
	matchingService.StartMatchingWorker(context.Background(), 5*time.Second)

	// Запускаем фоновые воркеры
	slaWorker := worker.NewSLAWorker(db, orderService, chatService, ledger).
		WithLeader(leader, "sla")
	slaWorker.Start(30 * time.Second)

	auctionWorker := worker.NewAuctionWorker(db, orderService).
		WithLeader(leader, "auction")
	auctionWorker.Start(1 * time.Minute)

	// Заказы без координат подачи — от старого клиента, который их не прислал и
	// чей адрес не удалось разрешить при создании, или заказы, появившиеся до
	// захвата координат, — не видны на карте исполнителя. Здесь они дозаполняются
	// через разрешатель адресов, вне пути запроса.
	geocodeWorker := worker.NewGeocodeBackfillWorker(orderRepo, addressSuggester).
		WithLeader(leader, "geocode_backfill")
	geocodeWorker.Start(1 * time.Minute)

	// Единственное, что закрывает истёкшую смену: один периодический проход, он же
	// подбирает смены, которые шли в момент перезапуска процесса.
	shiftWorker := worker.NewShiftWorker(shiftService).
		WithLeader(leader, "shift_autoclose")
	shiftWorker.Start(1 * time.Minute)

	// Здесь доменные события доходят до своих поведений: заказ, закрывающий себя
	// сам, когда его заказчик верифицирован, и идущее с этим вознаграждение.
	// Интервал короткий, потому что и того и другого кто-то ждёт.
	behaviorDispatcher := service.NewBehaviorDispatcher(
		eventRepo, orderRepo, userRepo, catalogRepo, serviceClaimRepo, chatRepo,
		settingsRepo, ledger, serviceBehaviors, orderService,
	).WithSubmissions(submissionRepo)
	behaviorWorker := worker.NewBehaviorWorker(behaviorDispatcher).
		WithLeader(leader, "behavior_dispatch").
		WithScriptSync(serviceBehaviors)
	behaviorWorker.Start(5 * time.Second)
	// Скрипты, отредактированные на другом процессе или прямо в базе, доходят до
	// этого в течение минуты.
	behaviorWorker.StartScriptSync(1 * time.Minute)

	// Ачивки читают тот же outbox, что и поведения, но со своим курсором.
	// Интервал длиннее: значок вполне может появиться минутой позже, а каждый
	// тик читает агрегаты по каждому субъекту события.
	achievementDispatcher := service.NewAchievementDispatcher(
		eventRepo, orderRepo, userRepo, achievementRepo, executorStatsRepo,
		giftRepo, mailRepo, incidentRepo, ledger, levels, achievementEngine,
	)
	achievementWorker := worker.NewAchievementWorker(achievementDispatcher).
		WithIncidents(incidentRepo).
		WithScriptSync(achievementScripts).
		WithLeader(leader, "achievement_dispatch")
	achievementWorker.Start(15 * time.Second)
	// Скрипты, отредактированные на другом процессе или прямо в базе, доходят до
	// этого в течение минуты — как и скрипты особых услуг.
	achievementWorker.StartScriptSync(1 * time.Minute)

	// Ночная проверка книг. Она только сообщает и никогда не чинит: баланс,
	// разошедшийся со своим реестром, — это баг, который надо видеть, а не число, которое надо переписать.
	reconcileWorker := worker.NewReconcileWorker(reconcileRepo, money.FromRubles(0.01)).
		WithLeader(leader, "reconcile")
	reconcileWorker.Start(24 * time.Hour)

	// Истёкшие refresh-токены удаляются ежедневно; использованные хранятся до
	// истечения срока, потому что обнаружение повторов должно их узнавать.
	go func() {
		authService.CleanupExpiredRefreshTokens(context.Background())
		for range time.Tick(24 * time.Hour) {
			authService.CleanupExpiredRefreshTokens(context.Background())
		}
	}()

	// Middleware
	authMiddleware := middleware.NewAuthMiddleware(userRepo, authService, jwtSecret)

	// Обработчики
	ph := handler.NewPublicHandler(authService)
	ah := handler.NewAdminHandler(adminService)
	oh := handler.NewOrderHandler(orderService)
	sh := handler.NewShiftHandler(shiftService)
	bh := handler.NewBidHandler(bidService, orderService)
	ch := handler.NewChatHandler(chatService)
	gh := handler.NewGeoHandler(addressSuggester)
	sch := handler.NewServiceCatalogHandler(catalogRepo).WithBehaviors(serviceBehaviors)
	arh := handler.NewAppReleaseHandler(appReleaseRepo, getEnv("RELEASES_DIR", "releases"), getEnv("RELEASES_BASE_URL", ""))
	rh := handler.NewReviewHandler(reviewService)
	egh := handler.NewExecutorGeoHandler(executorGeoService)
	bhh := handler.NewBehaviorHandler(behaviorDispatcher, submissionRepo)
	ach := handler.NewAchievementHandler(achievementRepo, giftRepo, mailRepo, executorStatsRepo, incidentRepo, levels, achievementEngine).
		WithScripts(achievementScripts)

	// Ограничители частоты для эндпоинтов, которые есть смысл перебирать.
	loginLimiter := middleware.NewRateLimiter(10, time.Minute)
	passwordResetLimiter := middleware.NewRateLimiter(5, 15*time.Minute)
	registerLimiter := middleware.NewRateLimiter(5, time.Hour)
	geoLimiter := middleware.NewRateLimiter(30, time.Minute)
	// Обновление сессии ограничивается отдельно и щедрее входа: это не подбор
	// учётных данных, а обмен уже выданного токена, и ключ здесь — адрес
	// клиента. За NAT мобильного оператора под одним адресом сидят сотни
	// приложений, и общий с /login лимит в 10 запросов в минуту отказывал бы им
	// в обновлении, то есть выбрасывал бы их из аккаунта.
	refreshLimiter := middleware.NewRateLimiter(120, time.Minute)

	r := chi.NewRouter()
	// StripQueryToken выполняется до логгера, чтобы учётные данные, переданные
	// параметром запроса, никогда не попадали в лог доступа.
	r.Use(middleware.StripQueryToken)
	r.Use(corsMiddleware)
	r.Use(middleware.SecurityHeaders)
	r.Use(chiMiddleware.Recoverer)
	// Внутри Recoverer, чтобы паника считалась той самой 500, которую клиент
	// действительно получил, а не пропадала из счётчиков запросов целиком.
	r.Use(metrics.Middleware)
	r.Use(chiMiddleware.Logger)
	r.Use(middleware.MaxBodyBytes(1 << 20))

	// registerAPIRoutes навешивает каждый обработчик на переданный chi.Router. Он
	// монтируется под /api/*, а пока включён LEGACY_ROOT_ROUTES — ещё и в корне,
	// для мобильных сборок, появившихся раньше префикса /api. Оба монтирования
	// несут одни и те же middleware аутентификации и авторизации.
	registerAPIRoutes := func(r chi.Router) {
		r.Get("/health", ph.HealthHandler)
		r.With(registerLimiter.Middleware).Post("/register", ph.RegisterHandler)
		r.With(loginLimiter.Middleware).Post("/login", ph.LoginHandler)
		// Обновление намеренно без аутентификации: к моменту, когда клиенту это нужно,
		// access-токен уже истёк. Учётными данными служит refresh-токен, поэтому
		// эндпоинт ограничен по частоте, как и прочие эндпоинты с учётными данными.
		r.With(refreshLimiter.Middleware).Post("/auth/refresh", ph.RefreshHandler)
		r.Get("/auth/verify-email", ph.VerifyEmailHandler)
		r.With(passwordResetLimiter.Middleware).Post("/auth/forgot-password", ph.ForgotPasswordHandler)
		r.With(passwordResetLimiter.Middleware).Post("/auth/reset-password", ph.ResetPasswordHandler)
		// Провайдер адресов — платный общий внешний сервис: неограниченный анонимный
		// доступ к нему жжёт квоту и замедляет ввод адреса для всех.
		r.With(geoLimiter.Middleware).Get("/geo/geocode", gh.Geocode)
		r.With(geoLimiter.Middleware).Get("/geo/autocomplete", gh.Autocomplete)
		r.With(geoLimiter.Middleware).Get("/geo/suggest", gh.Suggest)
		r.Get("/settings", ah.GetPublicSettingsHandler)
		// OptionalAuth, чтобы каталог мог прятать услуги «только для верифицированных»
		// от неверифицированных заказчиков, оставаясь доступным анонимным посетителям.
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.OptionalAuth)
			r.Get("/service-categories", sch.ListRootCategories)
			r.Get("/service-categories/{id}/children", sch.ListChildren)
			r.Get("/service-categories/{id}/variants", sch.ListCategoryVariants)
			r.Get("/service-variants", sch.ListVariants)
			r.Get("/service-variants/{id}", sch.GetVariant)
		})
		r.Get("/app/version", arh.GetVersionHandler)
		r.Get("/users/{id}/reviews", rh.GetUserReviews)
		r.Get("/users/{id}/rating", rh.GetUserRating)

		// Аутентифицированные маршруты заказчика. ADMIN включён, чтобы поддержка могла
		// действовать от имени заказчика из админ-панели.
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Use(middleware.RequireRole("CUSTOMER", "ADMIN"))
			r.Post("/customer/orders", oh.CreateOrderHandler)
			r.Post("/customer/orders/construction", bh.CreateConstructionOrderHandler)
			r.Post("/customer/orders/{id}/confirm", oh.ConfirmOrderHandler)
			r.Post("/customer/orders/{id}/tip", oh.TipOrderHandler)
			r.Post("/customer/orders/{id}/cancel", oh.CancelOrderHandler)
			r.Get("/customer/orders", oh.GetCustomerOrdersHandler)
			r.Post("/customer/bids/{id}/accept", bh.AcceptBidHandler)
			r.Get("/customer/orders/{id}/bids", bh.GetBidsHandler)
		})

		// Аутентифицированные общие маршруты (заказчик + исполнитель + админ)
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Use(middleware.RequireRole("CUSTOMER", "EXECUTOR", "ADMIN"))
			r.Get("/auth/me", ph.MeHandler)
			r.Get("/user/profile", ah.GetProfileHandler)
			// Оба пути возвращают собственный профиль вызывающего. /customer/profile
			// оставлен здесь, а не в группе заказчика, потому что приложение
			// исполнителя тоже его вызывает.
			r.Get("/customer/profile", ah.GetProfileHandler)
			// Исполнителям тоже нужны пополнения: штрафы могут увести баланс в минус.
			r.Post("/customer/finances/topup", ah.CreateTopUpRequestHandler)
			r.Post("/user/email", ph.UpdateEmailHandler)
			r.Post("/user/birth-date", ph.UpdateBirthDateHandler)
			r.With(passwordResetLimiter.Middleware).Post("/user/change-password", ph.ChangePasswordHandler)
			r.Post("/user/address", ah.AddAddressHandler)
			r.Post("/user/address/default", ah.SetDefaultAddressHandler)
			r.Delete("/user/address/{id}", ah.DeleteAddressHandler)
			r.Get("/chats/{order_id}/messages", ch.GetMessagesHandler)
			r.Post("/chats/{order_id}/messages", ch.SendMessageHandler)
			r.Put("/chats/{order_id}/messages/{message_id}", ch.EditMessageHandler)
			r.Delete("/chats/{order_id}/messages/{message_id}", ch.DeleteMessageHandler)
			r.Post("/chats/{order_id}/upload", ch.UploadAttachmentHandler)
			r.Post("/chats/{order_id}/read", ch.MarkReadHandler)
			r.Get("/chats/unread-summary", ch.GetUnreadSummaryHandler)
			// Внутренняя почта: сюда приходят выданные ачивки, купоны на
			// подарки, акции и новости. Она есть у всех ролей, потому что
			// новость адресуется человеку, а не его роли в заказе.
			r.Get("/user/mail", ach.GetMail)
			r.Get("/user/mail/unread", ach.GetMailUnread)
			r.Post("/user/mail/read-all", ach.MarkAllMailRead)
			r.Post("/user/mail/{id}/read", ach.MarkMailRead)
			r.Delete("/user/mail/{id}", ach.DeleteMail)
			r.Get("/chats/{order_id}/ws", ch.WebSocketHandler)
			r.Get("/support/chat", ch.GetUserSupportChatHandler)
			r.Get("/support/chats/{chat_id}/messages", ch.GetSupportMessagesHandler)
			r.Post("/support/chats/{chat_id}/messages", ch.SendSupportMessageHandler)
			r.Post("/support/chats/{chat_id}/upload", ch.UploadSupportAttachmentHandler)
			r.Post("/orders/{id}/reviews", rh.CreateReview)
			r.Get("/orders/{id}/reviews/mine", rh.GetOrderReview)
			r.Post("/finances/withdrawals", ah.CreateWithdrawalRequestHandler)
			r.Post("/logout", ph.LogoutHandler)
		})

		// Аутентифицированные маршруты исполнителя
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Use(middleware.RequireRole("EXECUTOR", "MODERATOR", "ADMIN"))
			r.Post("/executor/shifts", sh.StartShiftHandler)
			r.Post("/executor/shifts/end", sh.EndShiftHandler)
			r.Post("/executor/shifts/early-end", sh.EarlyEndShiftHandler)
			r.Post("/executor/shifts/location", sh.UploadLocationHandler)
			r.Post("/executor/set-location", egh.SetLocation)
			// Возобновляет автоматическое позиционирование после ручного выбора.
			r.Post("/executor/follow-device", egh.FollowDevice)
			r.Get("/executor/location", egh.GetLocation)
			r.Get("/executor/map-orders", egh.GetMapOrders)
			r.Get("/executor/shifts/active", sh.GetActiveShiftHandler)
			r.Get("/executor/history", sh.GetExecutorHistoryHandler)
			r.Get("/executor/orders/assigned", oh.GetExecutorAssignedOrdersHandler)
			r.Get("/executor/orders/available", bh.GetAvailableConstructionOrdersHandler)
			r.Get("/executor/orders/nearby", oh.GetNearbyOrdersHandler)
			r.Post("/executor/orders/{id}/accept", oh.AcceptOrder)
			r.Post("/executor/orders/{id}/execute", oh.ExecuteOrder)
			r.Post("/executor/orders/{id}/reject", oh.RejectOrderHandler)
			// Данные, которые исполнитель отправляет на проверку по скриптовой услуге, —
			// проверка личности в заказе верификации.
			r.Post("/executor/orders/{id}/submission", bhh.SubmitOrderData)
			r.Post("/executor/orders/{id}/bids", bh.CreateBidHandler)
			// Геймификация: значки, уровень со ставкой комиссии и подарки.
			r.Get("/executor/achievements", ach.GetAchievements)
			r.Get("/executor/level", ach.GetLevel)
			r.Get("/executor/gifts", ach.GetGifts)
			r.Post("/executor/gifts/{id}/reveal", ach.RevealGift)
		})

		// Аутентифицированные маршруты админа
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Use(authMiddleware.RequireAdmin)
			r.Get("/admin/geo-alerts", egh.GetGeoAlerts)
			r.Get("/admin/users", ah.GetUsersHandler)
			r.Post("/admin/users/{id}/status", ah.UpdateUserStatusHandler)
			r.Post("/admin/users/{id}/verified", ah.UpdateUserVerifiedHandler)
			r.Post("/admin/users/{id}/role", ah.UpdateUserRoleHandler)
			r.Post("/admin/users/{id}/roles", ah.UpdateUserRolesHandler)
			r.Post("/admin/users/{id}/address", ah.UpdateUserAddressHandler)
			r.Post("/admin/users/{id}/name", ah.UpdateUserNameHandler)
			r.Post("/admin/users/{id}/birth-date", ah.UpdateUserBirthDateHandler)
			r.Post("/admin/users/{id}/balance", ah.TopUpUserBalanceHandler)
			r.Get("/admin/finances/topups", ah.GetTopUpRequestsHandler)
			r.Post("/admin/finances/topups/{id}/approve", ah.ApproveTopUpRequestsHandler)
			r.Post("/admin/finances/topups/{id}/reject", ah.RejectTopUpRequestsHandler)
			r.Get("/admin/finances/withdrawals", ah.GetWithdrawalRequestsHandler)
			r.Post("/admin/finances/withdrawals/{id}/approve", ah.ApproveWithdrawalRequestsHandler)
			r.Post("/admin/finances/withdrawals/{id}/reject", ah.RejectWithdrawalRequestsHandler)
			r.Get("/admin/finances/commission", ah.GetCommissionHandler)
			r.Post("/admin/finances/commission/payout", ah.PayoutCommissionHandler)
			r.Get("/admin/transactions", ah.GetTransactionsHandler)
			r.Get("/admin/finances/reconciliation", ah.GetReconciliationHandler)
			r.Get("/admin/settings", ah.GetSettingsHandler)
			r.Post("/admin/settings", ah.UpdateSettingsHandler)
			r.Get("/admin/support/chats", ch.GetAdminSupportChatListHandler)
			r.Get("/admin/support/unread-summary", ch.GetAdminSupportUnreadSummaryHandler)
			r.Post("/admin/support/chats/{chat_id}/ban", ch.BanSupportChatHandler)
			r.Post("/admin/support/chats/{chat_id}/unban", ch.UnbanSupportChatHandler)
			r.Get("/admin/shifts/active", ah.GetActiveShiftsHandler)
			r.Get("/admin/orders/active", ah.GetActiveOrdersHandler)
			r.Get("/admin/orders/completed", ah.GetCompletedOrdersHandler)
			r.Get("/admin/escalations", bhh.ListEscalations)
			r.Post("/admin/escalations/{id}/resolve", bhh.ResolveEscalation)
			r.Get("/admin/service-behaviors", sch.AdminListBehaviors)
			r.Get("/admin/service-nodes", sch.AdminListNodes)
			r.Get("/admin/service-nodes/{id}", sch.AdminGetNode)
			r.Post("/admin/service-nodes", sch.AdminCreateNode)
			r.Put("/admin/service-nodes/{id}", sch.AdminUpdateNode)
			r.Delete("/admin/service-nodes/{id}", sch.AdminDeleteNode)
			r.Post("/admin/service-nodes/{id}/restore", sch.AdminRestoreNode)
			r.Post("/admin/app-releases", arh.UploadReleaseHandler)
			r.Post("/admin/broadcast-email", ah.SendBroadcastEmailHandler)
			r.Get("/admin/achievements", ach.AdminListAchievements)
			r.Post("/admin/achievements", ach.AdminCreateAchievement)
			r.Put("/admin/achievements/{code}", ach.AdminUpdateAchievement)
			r.Delete("/admin/achievements/{code}", ach.AdminDeleteAchievement)
			r.Post("/admin/achievements/{code}/restore", ach.AdminRestoreAchievement)
			r.Post("/admin/achievements/grants/{id}/revoke", ach.AdminRevokeAchievement)
			r.Get("/admin/users/{id}/achievements", ach.AdminUserAchievements)
			r.Post("/admin/users/{id}/stats/recalculate", ach.AdminRecalculateStats)
			r.Get("/admin/gifts", ach.AdminListGifts)
			r.Put("/admin/gifts/{code}", ach.AdminSaveGift)
			r.Post("/admin/gifts/{code}/codes", ach.AdminAddGiftCodes)
			r.Post("/admin/gifts/coupons/{coupon}/redeem", ach.AdminRedeemCoupon)
			r.Post("/admin/mail/broadcast", ach.AdminBroadcastMail)
			r.Get("/admin/finances/incidents", ach.AdminListIncidents)
			r.Post("/admin/finances/incidents/{id}/resolve", ach.AdminResolveIncident)
		})
	}

	// Основное монтирование: /api/* (веб через nginx + пересобранное мобильное приложение).
	r.Route("/api", registerAPIRoutes)

	// Легаси-монтирование: тот же API в корне, для установленных APK, появившихся
	// раньше префикса /api. По умолчанию выключено — снаружи до этих путей всё
	// равно не дотянуться: nginx проксирует только /api/, /health, /releases/ и
	// /uploads/, а обычный HTTP-порт, с которым они общались, больше не
	// публикуется. Ставьте LEGACY_ROOT_ROUTES=1 только если старого клиента снова пустили в сеть.
	if getEnv("LEGACY_ROOT_ROUTES", "0") == "1" {
		log.Println("LEGACY_ROOT_ROUTES enabled: the API is also served without the /api prefix, doubling the exposed surface.")
		registerAPIRoutes(r)
	}

	// APK релизов публичны по замыслу. Загруженные вложения чата — нет:
	// их отдаёт аутентифицированный обработчик, проверяющий, что вызывающий
	// участвует в переписке, которой принадлежит файл.
	r.Get("/releases/*", http.StripPrefix("/releases/", http.FileServer(http.Dir(getEnv("RELEASES_DIR", "releases")))).ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.RequireAuth)
		r.Get("/uploads/*", ch.ServeAttachmentHandler)
		r.Get("/api/uploads/*", ch.ServeAttachmentHandler)
	})

	// Цель сбора для Prometheus. Привязана только к сети compose: nginx её не
	// проксирует, а порт не публикуется на хост. Пустое значение METRICS_ADDR
	// выключает слушатель.
	metrics.Serve(getEnv("METRICS_ADDR", ":9091"), metrics.OpsHandlers{
		// Общий с ops-ботом, который единственный это вызывает.
		// Незаданное значение означает, что маршруты вообще не регистрируются.
		Secret: os.Getenv("OPS_KEY"),
		Reconcile: func() (any, error) {
			return adminService.Reconcile(context.Background(), money.FromRubles(0.01))
		},
	})

	// Регистрируем обработчики pprof для отладки (доступны только локально)
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	addr := getEnv("HTTP_ADDR", ":8080")
	certFile := getEnv("TLS_CERT_FILE", "")
	keyFile := getEnv("TLS_KEY_FILE", "")

	errChan := make(chan error, 2)

	srv := newServer(addr, r)
	if certFile != "" && keyFile != "" {
		go func() {
			log.Printf("Starting HTTPS server on %s", addr)
			errChan <- srv.ListenAndServeTLS(certFile, keyFile)
		}()
	} else {
		go func() {
			log.Printf("Starting HTTP server on %s", addr)
			errChan <- srv.ListenAndServe()
		}()
	}

	// Необязательный обычный HTTP-сервер для мобильных/отладочных клиентов в той же сети.
	// Задайте MOBILE_HTTP_ADDR (например, :8081), чтобы включить. По умолчанию выключен.
	if mobileAddr := getEnv("MOBILE_HTTP_ADDR", ""); mobileAddr != "" {
		go func() {
			log.Printf("Starting mobile HTTP server on %s", mobileAddr)
			errChan <- newServer(mobileAddr, r).ListenAndServe()
		}()
	}

	log.Fatalf("Server error: %v", <-errChan)
}

// newServer собирает http.Server с явными таймаутами. У сервера с нулевыми
// значениями их нет, что оставляет процесс открытым для истощения медленными клиентами.
// WriteTimeout намеренно отсутствует: WebSocket чата живёт на том же роутере,
// и дедлайн записи рвал бы долгоживущие сокеты.
func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// configurePool ограничивает пул соединений.
//
// Смысл в границе, а не в размере. У ненастроенного пула нет предела на
// открытые соединения, поэтому всплеск клиентов открывает их, пока Postgres не
// откажет «sorry, too many connections» — это авария, а не очередь. С пределом
// очередь образуется внутри процесса, где она видна (датчики пула,
// зарегистрированные metrics.RegisterDB, отдают WaitCount и WaitDuration) и где
// ждущие запросы сохраняют своё место в очереди.
//
// Idle намеренно держится равным open: умолчание в два простаивающих
// соединения означает, что каждый всплеск сверх двух снова платит за
// установку соединения, а именно этой платы всё и должно избежать. Умолчание
// в 25 оставляет под штатным max_connections=100 место для трафика воркеров,
// прогона миграций и сессии psql, и переопределяется для иначе настроенного хоста.
func configurePool(db *sql.DB) {
	maxOpen := getEnvInt("DB_MAX_OPEN_CONNS", 25)
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxOpen)
	// Перерабатываем простаивающие соединения, чтобы пул, раздувшийся на пике,
	// вернулся к небольшому устойчивому состоянию, и ограничиваем общее время жизни,
	// чтобы долгий процесс подхватывал изменения на сервере (перезапуск базы, смену пароля).
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	log.Printf("[db] pool limited to %d open connections", maxOpen)
}

// waitForDB повторяет db.Ping с короткой паузой, пока база не будет готова.
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
	// Общий с проверкой Origin у WebSocket, чтобы оба оставались согласованными.
	allowedOrigins := service.AllowedOrigins()
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

// getEnvInt читает положительную целочисленную настройку, откатываясь к
// умолчанию, если она не задана или не разбирается. Кривое значение забирает
// умолчание, а не процесс: ручки настройки не должны мешать сервису стартовать.
func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		log.Printf("[config] %s=%q is not a positive integer, using %d", key, v, fallback)
	}
	return fallback
}
