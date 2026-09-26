package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "net/http/pprof"

	"healthlogin/backend/achievement"
	"healthlogin/backend/achievements"
	"healthlogin/backend/behavior"
	"healthlogin/backend/behaviors"
	"healthlogin/backend/dbconn"
	"healthlogin/backend/handler"
	"healthlogin/backend/metrics"
	"healthlogin/backend/middleware"
	"healthlogin/backend/money"
	"healthlogin/backend/passport"
	"healthlogin/backend/perks"
	"healthlogin/backend/photoproof"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
	"healthlogin/backend/worker"
)

func main() {
	// Сигнал завершения отменяет этот контекст: на него смотрят воркеры, а
	// HTTP-серверы по нему выключаются штатно. До сих пор SIGTERM обрывал
	// процесс посреди тика — у SLA это между фиксацией возврата и уведомлением.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := dbconn.OpenFromEnv(ctx)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	configurePool(db)

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
	// Админские выборки по доменам: пользователи, заявки на деньги, журнал
	// проводок, список заказов, активные смены. Один «репозиторий админа» на
	// всё вынуждал каждый мок повторять все двадцать методов.
	adminUserRepo := repository.NewAdminUserRepository(db)
	payoutRepo := repository.NewPayoutRepository(db)
	transactionJournal := repository.NewTransactionJournal(db)
	adminOrderRepo := repository.NewAdminOrderRepository(db)
	shiftMonitorRepo := repository.NewShiftMonitorRepository(db)
	// Справочник ролей и их прав. На него опираются и назначение ролей, и охрана
	// каждого админского маршрута.
	roleRepo := repository.NewRoleRepository(db)
	penaltyRepo := repository.NewPenaltyRepository(db)
	disputeRepo := repository.NewDisputeRepository(db)
	// system_settings — несколько строк, читаемых на путях ценообразования,
	// допуска и подбора, по нескольку раз за запрос и внутри циклов воркеров. Кэш
	// сквозной, поэтому правка админа всё равно применится к следующему заказу;
	// TTL лишь ограничивает устаревание от записей, сделанных не этим процессом.
	settingsRepo := repository.NewCachedSettingsRepository(
		repository.NewSettingsRepository(db),
		time.Duration(getEnvInt("SETTINGS_CACHE_TTL_SEC", 10))*time.Second,
	)
	// Фото-подтверждение — отдельный модуль со своей схемой и своими правилами.
	photoProofService := photoproof.NewService(photoproof.NewSymbolRepository(db)).
		WithTrack(photoproof.NewTrackRepository(db), settingsRepo).
		WithProofs(db, photoproof.DiskStorage{Root: getEnv("UPLOADS_DIR", "uploads")}, photoproof.NewChecker())
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
	// Магазин: каталог, покупки и привилегии на комиссию.
	shopRepo := repository.NewShopRepository(db)
	perkRepo := repository.NewPerkRepository(db)
	shopOrderRepo := repository.NewShopOrderRepository(db)

	// Сервисы
	// Любое движение денег идёт через реестр, который всегда затрагивает и баланс
	// пользователя, и системный счёт.
	ledger := service.NewLedger(transactionRepo, systemAccountRepo).
		WithIncidents(incidentRepo)
	// Штрафные баллы: журнал и свёрнутое состояние ролей. Транзакции берёт у
	// реестра — баллы по спору начисляются в одной транзакции с деньгами.
	penaltyService := service.NewPenaltyService(penaltyRepo, userRepo, settingsRepo, ledger)

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
	// Привилегия магазина применяется здесь же, после уровня: это та же точка,
	// через которую ходит подтверждение заказа и решение спора.
	// Правила привилегий — скрипты; поставляемые компилируются здесь, и
	// сломанное поставляемое правило — ошибка сборки, а не повод стартовать:
	// на нём стоят купленные привилегии.
	perkRules, err := service.NewPerkRules(repository.NewPerkRuleRepository(db), settingsRepo, perks.FS)
	if err != nil {
		log.Fatalf("[perk] shipped rules: %v", err)
	}
	levels := service.NewLevels(achievementRepo, settingsRepo).
		WithPerks(perkRepo, perkRules, incidentRepo)

	// DaData — единственный источник адресных данных: и подсказок, и разрешения
	// координат. Запасного варианта намеренно нет: у альтернативы не было данных о
	// квартирах и она отвергала обычные номера домов, а молчаливое возвращение к
	// ней спрятало бы ошибку конфигурации за наполовину работающим вводом адреса.
	// Кэш избавляет провайдера от повторных разрешений одного и того же адреса на
	// запасном пути.
	addressSuggester := service.NewAddressSuggester(service.NewDaData(daDataConfigFromEnv()), repository.NewGeocodeCacheRepository(db))
	if addressSuggester.Configured() {
		log.Printf("[address] suggestions served by DaData")
	} else {
		// Не фатально: отсутствующий ключ не должен утаскивать за собой заказы, чат и
		// платежи. Ввод адреса отдаёт 503, пока ключ не задан.
		log.Printf("[address] WARNING: DADATA_API_KEY is not set — address suggestions will return 503 and registration cannot complete")
	}
	// Почта наружу и доверенные источники браузера собираются здесь, из
	// окружения: сервисы переменных не читают.
	mailer := service.NewSmtpMailSender(smtpConfigFromEnv())
	origins := service.NewAllowedOrigins(getEnv("CORS_ORIGIN", ""))
	// AuthService владеет всем, что связано с сессиями: выдачей access-токенов,
	// ротацией refresh-токенов и занесением отозванных access-токенов в чёрный список.
	// Редакцию согласия на обработку персональных данных он читает из настроек
	// напрямую: сервис паспортов собирается позже, после заказов.
	authService := service.NewAuthServiceWithSecret(userRepo, jwtSecret, addressSuggester, mailer).
		WithAddresses(addressRepo).
		WithExecutorGeo(executorGeoRepo).
		WithSessionStorage(refreshRepo, tokenRepo).
		WithConsent(func(ctx context.Context) int { return service.PDConsentVersion(ctx, settingsRepo) })
	adminService := service.NewAdminService(userRepo, adminUserRepo, settingsRepo, mailer).
		WithPayouts(payoutRepo).
		WithJournal(transactionJournal).
		WithOrders(adminOrderRepo).
		WithShifts(shiftMonitorRepo).
		WithSessions(authService).
		WithLedger(ledger).
		WithAddresses(addressRepo).
		WithReconciliation(reconcileRepo).
		WithEvents(eventRepo).
		WithRoles(roleRepo).
		WithPenalties(penaltyRepo)
	// Самообслуживание пользователя: профиль с адресами и собственные заявки
	// на пополнение и вывод. Не админские операции — и не в AdminService.
	profileService := service.NewProfileService(userRepo, addressRepo).WithSettings(settingsRepo)
	walletService := service.NewWalletService(userRepo, payoutRepo, ledger)
	// Права: что разрешено роли, отличной от ADMIN. Кэш карты «роль → права»
	// сбрасывается тем же, что её меняет, — страницей ролей.
	permissions := service.NewPermissions(roleRepo)
	roleService := service.NewRoleService(roleRepo, userRepo, adminUserRepo, permissions).
		WithSessions(authService)
	disputeNotifier := service.NewDisputeNotifier(mailRepo, userRepo, mailer)
	// Радиус взятия настраивается в админке; ACCEPT_RADIUS_KM — запасное
	// значение для установок, поднятых до появления настройки.
	acceptRadiusFallbackKM := acceptRadiusFromEnv()
	orderService := service.NewOrderService(orderRepo, ledger, settingsRepo, userRepo, shiftRepo, chatRepo, catalogRepo, addressSuggester).
		WithAcceptRadiusFallback(acceptRadiusFallbackKM).
		WithExecutorGeo(executorGeoRepo).
		WithBehaviors(serviceBehaviors, serviceClaimRepo, eventRepo).
		WithAchievements(levels, executorStatsRepo).
		WithDisputes(disputeRepo).
		WithPenalties(penaltyService).
		WithDisputeNotifier(disputeNotifier).
		WithPhotoProof(photoProofService)
	// Споры закрывают заказ теми же шагами, что и заказчик: через жизненный
	// цикл заказа, а не мимо него.
	disputeService := service.NewDisputeService(orderService, orderRepo, ledger, disputeRepo, catalogRepo, chatRepo).
		WithPenalties(penaltyService).
		WithNotifier(disputeNotifier).
		WithEvidence(photoProofService, executorGeoRepo, settingsRepo)
	// Карта берёт заказы у сервиса заказов: у неё и списка «Заказы поблизости»
	// одна реализация.
	executorGeoService := service.NewExecutorGeoService(executorGeoRepo, settingsRepo).
		WithAcceptRadiusFallback(acceptRadiusFallbackKM).
		WithNearbyOrders(orderService).
		WithTrack(photoProofService)
	// Отчёты о местоположении в смене пишутся через гео-сервис, поэтому у
	// сохранённой позиции исполнителя один писатель и один набор правил.
	shiftService := service.NewShiftService(shiftRepo, ledger, settingsRepo, orderRepo).
		WithExecutorLocation(executorGeoService).
		WithOrderHistory(orderService)
	// Автоматический подбор ограничен расстоянием, для чего нужны сохранённая
	// позиция исполнителя и настроенный радиус.
	matchingService := service.NewMatchingService(orderRepo, shiftRepo, userRepo, catalogRepo).
		WithGeo(executorGeoRepo, settingsRepo).
		WithBehaviors(serviceBehaviors).
		WithPenalties(penaltyService)
	bidService := service.NewBidService(bidRepo, orderRepo, shiftRepo, ledger, userRepo, catalogRepo, chatRepo).
		WithBehaviors(serviceBehaviors, eventRepo).
		WithPenalties(penaltyService)
	chatService := service.NewChatService(chatRepo, orderRepo).WithOrigins(origins)
	// Магазин платит тем же реестром и выдаёт вещи теми же подарками, что и
	// ачивки: склад у них один. Четыре сервиса — по разделам прав: витрина и
	// покупка, каталог, обработка покупок с выручкой, выдача привилегий.
	shop := service.NewShop(shopRepo, shopOrderRepo, perkRepo, perkRules, giftRepo, ledger, levels, settingsRepo).
		WithEvents(eventRepo).
		WithMail(mailRepo)
	shopCatalog := service.NewShopCatalog(shopRepo, giftRepo, perkRules).WithRoles(roleRepo)
	shopOrders := service.NewShopOrders(shopOrderRepo, giftRepo, perkRepo, ledger).WithMail(mailRepo)
	perkGrants := service.NewPerkGrants(perkRepo, perkRules, ledger).WithMail(mailRepo)
	reviewService := service.NewReviewService(reviewRepo, orderRepo).
		WithTx(ledger).
		WithExecutorStats(executorStatsRepo)

	// Здесь доменные события доходят до своих поведений: заказ, закрывающий себя
	// сам, когда его заказчик верифицирован, и идущее с этим вознаграждение.
	// Диспетчер — потребитель outbox; синхронный поток отправок по заказу
	// (данные на проверку, паспорт заказчика) — отдельный OrderSubmissions,
	// которому диспетчер подключён как обработчик уже опубликованного события.
	behaviorDispatcher := service.NewBehaviorDispatcher(service.BehaviorDispatcherDeps{
		Events: eventRepo, Orders: orderRepo, Users: userRepo, Catalog: catalogRepo,
		Claims: serviceClaimRepo, Chat: chatRepo, Settings: settingsRepo, Ledger: ledger,
		Behaviors: serviceBehaviors, OrderLifecycle: orderService,
	}).WithSubmissions(submissionRepo)
	passportRepo := repository.NewPassportRepository(db)
	orderSubmissions := service.NewOrderSubmissions(orderRepo, userRepo, catalogRepo, submissionRepo,
		eventRepo, ledger, serviceBehaviors, behaviorDispatcher).
		WithPassports(passportRepo)
	// Паспорта шифруются ключом из окружения. Без ключа сервер стартует, а приём
	// паспортов отвечает 503: хранить паспорт открытым нельзя даже временно.
	passportCipher, err := passport.NewCipher(getEnv("PASSPORT_ENC_KEY", ""), getEnvInt("PASSPORT_KEY_VERSION", 1))
	if err != nil {
		log.Fatalf("[passport] %v", err)
	}
	if passportCipher == nil {
		log.Println("[passport] WARNING: PASSPORT_ENC_KEY is not set, passports are not accepted")
	}
	passportService := service.NewPassportService(passportRepo, userRepo, passportCipher,
		getEnv("PASSPORTS_DIR", "passports"), settingsRepo).
		WithMail(mailRepo).
		WithPhotoCheck(photoproof.NewChecker()).
		WithVerification(orderSubmissions)

	// Каждая периодическая задача ниже меняет состояние, которое должно измениться
	// один раз: возврат, штраф, назначение. Защита лидером заставляет каждый тик
	// выполняться на одном процессе, поэтому вторая реплика его пропускает, а не
	// делает работу дважды. С одним процессом это стоит одной advisory-блокировки на тик и больше ничего.
	leader := worker.NewLeader(db)
	// Все воркеры живут на ctx и собираются здесь: при выключении main ждёт,
	// пока каждый доведёт начатый проход до конца.
	var workers worker.Group

	// Запускаем фоновый подборщик заказов
	workers.Add(worker.NewMatchingWorker(matchingService).
		WithLeader(leader, "matching").
		Start(ctx, 5*time.Second))

	// Запускаем фоновые воркеры
	workers.Add(worker.NewSLAWorker(orderService, chatService).
		WithLeader(leader, "sla").
		Start(ctx, 30*time.Second))

	workers.Add(worker.NewAuctionWorker(orderService).
		WithLeader(leader, "auction").
		Start(ctx, 1*time.Minute))

	// Заказы без координат подачи — от старого клиента, который их не прислал и
	// чей адрес не удалось разрешить при создании, или заказы, появившиеся до
	// захвата координат, — не видны на карте исполнителя. Здесь они дозаполняются
	// через разрешатель адресов, вне пути запроса.
	workers.Add(worker.NewGeocodeBackfillWorker(orderRepo, addressSuggester).
		WithLeader(leader, "geocode_backfill").
		Start(ctx, 1*time.Minute))

	// Единственное, что закрывает истёкшую смену: один периодический проход, он же
	// подбирает смены, которые шли в момент перезапуска процесса.
	workers.Add(worker.NewShiftWorker(shiftService).
		WithLeader(leader, "shift_autoclose").
		Start(ctx, 1*time.Minute))

	// Диспетчер поведений. Интервал короткий, потому что того, что он несёт,
	// кто-то ждёт.
	behaviorWorker := worker.NewBehaviorWorker(behaviorDispatcher).
		WithLeader(leader, "behavior_dispatch").
		WithScriptSync(serviceBehaviors)
	workers.Add(behaviorWorker.Start(ctx, 5*time.Second))
	// Скрипты, отредактированные на другом процессе или прямо в базе, доходят до
	// этого в течение минуты.
	workers.Add(behaviorWorker.StartScriptSync(ctx, 1*time.Minute))

	// Ачивки читают тот же outbox, что и поведения, но со своим курсором.
	// Интервал длиннее: значок вполне может появиться минутой позже, а каждый
	// тик читает агрегаты по каждому субъекту события.
	achievementDispatcher := service.NewAchievementDispatcher(service.AchievementDispatcherDeps{
		Events: eventRepo, Orders: orderRepo, Users: userRepo, Achievements: achievementRepo,
		Stats: executorStatsRepo, Gifts: giftRepo, Mail: mailRepo, Incidents: incidentRepo,
		Ledger: ledger, Levels: levels, Engine: achievementEngine,
	})
	achievementWorker := worker.NewAchievementWorker(achievementDispatcher).
		WithScriptSync(achievementScripts).
		WithLeader(leader, "achievement_dispatch")
	workers.Add(achievementWorker.Start(ctx, 15*time.Second))
	// Скрипты, отредактированные на другом процессе или прямо в базе, доходят до
	// этого в течение минуты — как и скрипты особых услуг.
	workers.Add(achievementWorker.StartScriptSync(ctx, 1*time.Minute))

	// Датчики, читаемые из базы на каждом процессе: открытые денежные
	// инциденты (на них алерт) и очереди обоих потребителей outbox. Из тика под
	// блокировкой лидера они публиковались бы только на одной реплике.
	workers.Add(worker.NewGaugeWorker().
		WithIncidents(incidentRepo).
		WithBehaviorBacklog(behaviorDispatcher).
		WithAchievementBacklog(achievementDispatcher).
		Start(ctx, 30*time.Second))

	// Сроки штрафов — время, а не событие: баллы сгорают, а тихие блокировки
	// снимаются сами, даже если человеку больше ничего не начисляют.
	workers.Add(worker.NewPenaltyWorker(penaltyService).
		WithTrack(photoProofService).
		WithLeader(leader, "penalty_sweep").
		Start(ctx, 1*time.Hour))

	// Напоминание о конце привилегии магазина за три дня.
	workers.Add(worker.NewPerkReminderWorker(perkGrants).
		WithLeader(leader, "perk_reminder").
		Start(ctx, 10*time.Minute))

	// Ночная проверка книг. Она только сообщает и никогда не чинит: баланс,
	// разошедшийся со своим реестром, — это баг, который надо видеть, а не число, которое надо переписать.
	workers.Add(worker.NewReconcileWorker(reconcileRepo, money.FromRubles(0.01)).
		WithLeader(leader, "reconcile").
		Start(ctx, 24*time.Hour))

	// Истёкшие refresh-токены удаляются ежедневно; использованные хранятся до
	// истечения срока, потому что обнаружение повторов должно их узнавать.
	workers.Add(worker.NewRefreshTokenWorker(authService).
		WithLeader(leader, "refresh_token_cleanup").
		Start(ctx, 24*time.Hour))

	// Middleware
	// Кэш пользователя в middleware: AUTH_CACHE_TTL_SEC=0 выключает его.
	authMiddleware := middleware.NewAuthMiddleware(userRepo, authService, jwtSecret, authCacheTTLFromEnv()).
		WithPermissions(permissions)

	// Каталоги, файлы и почта. Пути к загрузкам и релизам читаются здесь один
	// раз и передаются в обработчики: на пути запроса окружение не читается.
	uploadsDir := getEnv("UPLOADS_DIR", "uploads")
	releasesDir := getEnv("RELEASES_DIR", "releases")
	serviceCatalog := service.NewServiceCatalog(catalogRepo).
		WithBehaviors(serviceBehaviors).
		WithPenalties(penaltyService)
	serviceCatalogAdmin := service.NewServiceCatalogAdmin(catalogRepo, serviceBehaviors)
	appReleases := service.NewAppReleases(appReleaseRepo, releasesDir, getEnv("RELEASES_BASE_URL", ""))
	mailService := service.NewMail(mailRepo, userRepo)
	// Геймификация глазами экранов: каталог ачивок, склад подарков, инциденты.
	// Выдача остаётся у диспетчера.
	achievementCatalog := service.NewAchievementCatalog(achievementRepo, executorStatsRepo, levels, achievementEngine, achievementScripts)
	giftCatalog := service.NewGiftCatalog(giftRepo).WithShop(shopOrders)
	moneyIncidents := service.NewMoneyIncidents(incidentRepo)

	// Обработчики
	handlers := appHandlers{
		public:   handler.NewPublicHandler(authService).WithPermissions(permissions).WithPassports(passportService),
		passport: handler.NewPassportHandler(passportService),
		admin:    handler.NewAdminHandler(adminService),
		profile:  handler.NewProfileHandler(profileService),
		wallet:   handler.NewWalletHandler(walletService),
		roles:    handler.NewRoleHandler(roleService),
		orders:   handler.NewOrderHandler(orderService),
		executorVerification: handler.NewExecutorVerificationHandler(service.NewExecutorVerificationService(
			userRepo, addressRepo, catalogRepo, orderRepo, serviceBehaviors, orderService, ledger)),
		shifts:         handler.NewShiftHandler(shiftService),
		bids:           handler.NewBidHandler(bidService, orderService),
		chat:           handler.NewChatHandler(chatService, uploadsDir).WithShopLinks(shopOrders.ShopOrderLinks),
		shop:           handler.NewShopHandler(shop, shopCatalog, shopOrders, perkGrants, perkRules, uploadsDir),
		geo:            handler.NewGeoHandler(addressSuggester),
		serviceCatalog: handler.NewServiceCatalogHandler(serviceCatalog, serviceCatalogAdmin),
		appReleases:    handler.NewAppReleaseHandler(appReleases),
		reviews:        handler.NewReviewHandler(reviewService),
		executorGeo:    handler.NewExecutorGeoHandler(executorGeoService),
		behavior:       handler.NewBehaviorHandler(orderSubmissions),
		disputes:       handler.NewDisputeHandler(disputeService),
		penalties:      handler.NewPenaltyHandler(penaltyService),
		photoProof:     photoproof.NewHandler(photoProofService, handler.CallerID),
		mail:           handler.NewMailHandler(mailService),
		achievements:   handler.NewAchievementHandler(achievementCatalog, giftCatalog, moneyIncidents, achievementDispatcher),
	}

	// Легаси-монтирование: тот же API в корне, для установленных APK, появившихся
	// раньше префикса /api. По умолчанию выключено — снаружи до этих путей всё
	// равно не дотянуться: nginx проксирует только /api/, /health, /releases/ и
	// /uploads/, а обычный HTTP-порт, с которым они общались, больше не
	// публикуется. Ставьте LEGACY_ROOT_ROUTES=1 только если старого клиента снова пустили в сеть.
	legacyRoot := getEnv("LEGACY_ROOT_ROUTES", "0") == "1"
	if legacyRoot {
		log.Println("LEGACY_ROOT_ROUTES enabled: the API is also served without the /api prefix, doubling the exposed surface.")
	}
	r := newRouter(handlers, routerConfig{
		auth:         authMiddleware,
		allowsOrigin: origins.Allows,
		limits: limiters{
			login:         middleware.NewRateLimiter(10, time.Minute),
			passwordReset: middleware.NewRateLimiter(5, 15*time.Minute),
			register:      middleware.NewRateLimiter(5, time.Hour),
			geo:           middleware.NewRateLimiter(30, time.Minute),
			refresh:       middleware.NewRateLimiter(120, time.Minute),
			shopPurchase:  middleware.NewRateLimiter(30, time.Minute),
		},
		releasesDir:  releasesDir,
		legacyRoot:   legacyRoot,
		maxBodyBytes: 1 << 20,
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
	servers := []*http.Server{}

	srv := newServer(addr, r)
	servers = append(servers, srv)
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
		mobile := newServer(mobileAddr, r)
		servers = append(servers, mobile)
		go func() {
			log.Printf("Starting mobile HTTP server on %s", mobileAddr)
			errChan <- mobile.ListenAndServe()
		}()
	}

	select {
	case err := <-errChan:
		log.Fatalf("Server error: %v", err)
	case <-ctx.Done():
	}
	stop()
	log.Printf("Shutting down: waiting up to %v for requests and workers", shutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for _, s := range servers {
		if err := s.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown: %v", err)
		}
	}
	if err := workers.Wait(shutdownCtx); err != nil {
		log.Printf("Workers did not finish in time: %v", err)
	}
	log.Println("Shutdown complete")
}

// shutdownTimeout — сколько выключение ждёт текущие запросы и начатые проходы
// воркеров. Меньше, чем даёт оркестратор до SIGKILL, чтобы defer'ы выше
// (закрытие базы) ещё успели выполниться.
const shutdownTimeout = 15 * time.Second

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

// smtpConfigFromEnv собирает параметры SMTP из окружения. Пустой SMTP_HOST
// означает «почты нет»: письма отказывают, а не уходят в никуда.
func smtpConfigFromEnv() service.SmtpConfig {
	return service.SmtpConfig{
		Host:     getEnv("SMTP_HOST", ""),
		Port:     getEnv("SMTP_PORT", "587"),
		User:     getEnv("SMTP_USER", ""),
		Password: getEnv("SMTP_PASSWORD", ""),
		From:     getEnv("SMTP_FROM", "system@moya-usluga.ru"),
		BaseURL:  getEnv("APP_BASE_URL", "https://moya-usluga.ru"),
	}
}

// getEnv — dbconn.Env: один ридер окружения на все бинарники.
func getEnv(key, fallback string) string { return dbconn.Env(key, fallback) }

// authCacheTTLFromEnv читает AUTH_CACHE_TTL_SEC (по умолчанию 5 с). Ноль —
// допустимое значение, выключающее кэш, поэтому не getEnvInt.
func authCacheTTLFromEnv() time.Duration {
	if v := os.Getenv("AUTH_CACHE_TTL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
		log.Printf("[config] AUTH_CACHE_TTL_SEC=%q is not a non-negative integer, using 5", v)
	}
	return 5 * time.Second
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

// acceptRadiusFromEnv читает запасной радиус взятия из ACCEPT_RADIUS_KM.
// Пусто, мусор или неположительное значение — ноль, то есть умолчание сервиса.
func acceptRadiusFromEnv() float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(getEnv("ACCEPT_RADIUS_KM", "")), 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

// daDataConfigFromEnv читает параметры провайдера подсказок адресов. Мусор в
// DADATA_MAX_CONCURRENCY — ноль, то есть умолчание провайдера.
func daDataConfigFromEnv() service.DaDataConfig {
	n, _ := strconv.Atoi(strings.TrimSpace(getEnv("DADATA_MAX_CONCURRENCY", "")))
	return service.DaDataConfig{APIKey: getEnv("DADATA_API_KEY", ""), MaxConcurrency: n}
}
