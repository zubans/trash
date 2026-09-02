package service_test

import (
	"context"
	"database/sql"
	"math"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("skipping e2e database test: DATABASE_URL / TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("cannot ping database: %v", err)
	}
	if err := repository.Migrate(db, "../migrations"); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	return db
}

func TestE2E_CustomerExecutorFlow(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Репозитории
	userRepo := repository.New(db)
	orderRepo := repository.NewOrderRepository(db)
	shiftRepo := repository.NewShiftRepository(db)
	transactionRepo := repository.NewTransactionRepository(db)
	systemAccountRepo := repository.NewSystemAccountRepository(db)
	catalogRepo := repository.NewServiceCatalogRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	executorGeoRepo := repository.NewExecutorGeoRepository(db)
	addressRepo := repository.NewAddressRepository(db)

	ledger := service.NewLedger(transactionRepo, systemAccountRepo)

	authService := service.NewAuthServiceWithSecret(userRepo, "test-secret-key-12345", nil, nil).
		WithAddresses(addressRepo).
		WithExecutorGeo(executorGeoRepo)

	orderService := service.NewOrderService(orderRepo, ledger, settingsRepo, userRepo, shiftRepo, nil, catalogRepo, nil).
		WithExecutorGeo(executorGeoRepo)

	executorGeoService := service.NewExecutorGeoService(executorGeoRepo, orderRepo).
		WithEligibility(userRepo, settingsRepo, catalogRepo)

	// Подключено ровно так же, как в main.go: отчёты о местоположении в смене
	// пишутся через гео-сервис — именно это делает позицию, которую читают карта и
	// подбор, той же, что сообщило приложение исполнителя.
	shiftService := service.NewShiftService(shiftRepo, ledger, settingsRepo, orderRepo, catalogRepo, db).
		WithExecutorLocation(executorGeoService)

	// Активный вариант с ценой, который можно заказать. Каталог хранит названия
	// как JSONB и требует цену у VARIANT, поэтому строка собрана под оба условия.
	variantID := uuid.New()
	_, err := db.Exec(
		`INSERT INTO service_nodes (id, code, name, node_type, base_price, is_active)
		 VALUES ($1, $2, $3::jsonb, 'VARIANT', 100, true)`,
		variantID,
		"e2e-variant-"+uuid.New().String()[:8],
		`{"ru": "Стандартный вывоз", "en": "Standard pickup"}`,
	)
	if err != nil {
		t.Fatalf("failed to insert test service variant: %v", err)
	}

	// 1. Регистрируем заказчика с адресом
	custPhone := "+7999" + uuid.New().String()[:7]
	custEmail := "cust_" + uuid.New().String()[:8] + "@test.com"
	custAddress := "Россия, г. Москва, ул. Арбат, д. 10"
	custLat, custLon := 55.7512, 37.6000

	customer, err := authService.RegisterWithCoordinates(
		ctx, custPhone, custEmail, "Password123!",
		"Иванов", "Иван", "Иванович",
		"1990-05-17", custAddress, "CUSTOMER", &custLat, &custLon,
	)
	if err != nil {
		t.Fatalf("Customer registration failed: %v", err)
	}

	// Проверяем, что адрес заказчика лежит в единой таблице `addresses`
	custAddrs, err := addressRepo.List(ctx, customer.ID)
	if err != nil || len(custAddrs) == 0 {
		t.Fatalf("Customer address was not saved in addresses table: %v", err)
	}
	if !custAddrs[0].IsDefault {
		t.Errorf("Customer address should be default")
	}
	if custAddrs[0].Lat == nil || *custAddrs[0].Lat != custLat {
		t.Errorf("Customer address coordinates mismatch")
	}

	// Пополняем баланс заказчика, чтобы удержание прошло
	_ = userRepo.UpdateBalance(ctx, customer.ID, money.FromRubles(5000))
	_ = userRepo.UpdateVerified(ctx, customer.ID, true)

	// 2. Заказчик создаёт заказ со своим адресом
	order, err := orderService.CreateOrder(
		ctx, customer.ID, variantID, false, false,
		custAddress, &custLat, &custLon,
	)
	if err != nil {
		t.Fatalf("Customer order creation failed: %v", err)
	}
	if order.Status != repository.OrderStatusSearching {
		t.Errorf("expected order status SEARCHING, got %s", order.Status)
	}

	// 3. Регистрируем исполнителя с адресом поблизости (~150 метров)
	execPhone := "+7999" + uuid.New().String()[:7]
	execEmail := "exec_" + uuid.New().String()[:8] + "@test.com"
	execAddress := "Россия, г. Москва, ул. Новый Арбат, д. 2"
	execLat, execLon := 55.7520, 37.6010

	executor, err := authService.RegisterWithCoordinates(
		ctx, execPhone, execEmail, "Password123!",
		"Петров", "Петр", "Петрович",
		"1990-05-17", execAddress, "EXECUTOR", &execLat, &execLon,
	)
	if err != nil {
		t.Fatalf("Executor registration failed: %v", err)
	}

	// Проверяем, что адрес исполнителя лежит в единой таблице `addresses`
	execAddrs, err := addressRepo.List(ctx, executor.ID)
	if err != nil || len(execAddrs) == 0 {
		t.Fatalf("Executor address was not saved in addresses table: %v", err)
	}
	_ = userRepo.UpdateVerified(ctx, executor.ID, true)
	birthDate := time.Now().AddDate(-25, 0, 0)
	_ = userRepo.UpdateUserBirthDate(ctx, executor.ID, birthDate)

	// 4. Исполнитель открывает активную смену
	shift, err := shiftService.StartShift(ctx, executor.ID, 3)
	if err != nil {
		t.Fatalf("Executor start shift failed: %v", err)
	}
	if shift.Status != repository.ShiftStatusActive {
		t.Errorf("expected shift status ACTIVE, got %s", shift.Status)
	}

	// Приложение исполнителя сообщает свою позицию через эндпоинт смены. Это тот
	// самый путь, который раньше принимал координаты и выбрасывал их, поэтому
	// тест идёт через него, а не пишет в репозиторий напрямую.
	//
	// Сообщаемая точка намеренно НЕ та, что сохранила регистрация: если бы отчёт
	// отбрасывался, позиция всё равно читалась бы как координата регистрации, и
	// проверка прошла бы при сломанной фиче.
	// Перемещение около 55 м, внутри радиуса допуска, поэтому это обычное
	// обновление позиции, а не смена района.
	movedLat, movedLon := execLat+0.0005, execLon
	stored, err := shiftService.RecordLocation(ctx, executor.ID, movedLat, movedLon)
	if err != nil {
		t.Fatalf("failed to record executor location: %v", err)
	}
	if !stored {
		t.Fatal("the reported position was not stored")
	}

	// Она обязана прочитаться как авторитетная позиция исполнителя — сообщённая
	// точка, а не та, что оставила после себя регистрация.
	gotLat, gotLon, _, err := executorGeoRepo.GetExecutorLocation(ctx, executor.ID)
	if err != nil {
		t.Fatalf("failed to read executor location: %v", err)
	}
	if gotLat == nil || gotLon == nil {
		t.Fatal("executor position was not persisted by the shift location report")
	}
	if math.Abs(*gotLat-movedLat) > 1e-6 || math.Abs(*gotLon-movedLon) > 1e-6 {
		t.Errorf("stored position (%f, %f) is not the reported one (%f, %f) — the report was dropped",
			*gotLat, *gotLon, movedLat, movedLon)
	}
	// Дальше исполнитель находится в позиции, которую он сообщил.
	execLat, execLon = movedLat, movedLon

	// 5. Исполнитель запрашивает доступные заказы на карте
	mapOrders, err := executorGeoService.GetMapOrders(ctx, executor.ID)
	if err != nil {
		t.Fatalf("GetMapOrders failed: %v", err)
	}
	if len(mapOrders) == 0 {
		t.Fatalf("expected to find customer's order on the map, got 0 orders")
	}

	// Заказ обязан появиться на том расстоянии, которое реально следует из двух
	// пар координат, — вот что значит «видит его в правильном месте».
	wantKM := service.HaversineDistanceKM(execLat, execLon, custLat, custLon)
	found := false
	for _, o := range mapOrders {
		if o.ID == order.ID {
			found = true
			if math.Abs(o.DistanceKM-wantKM) > 0.01 {
				t.Errorf("map reports the order %.3f km away, expected %.3f km", o.DistanceKM, wantKM)
			}
			if o.PickupLat == nil || o.PickupLon == nil {
				t.Fatal("the order on the map carries no pickup coordinates")
			}
			if math.Abs(*o.PickupLat-custLat) > 1e-6 || math.Abs(*o.PickupLon-custLon) > 1e-6 {
				t.Errorf("order pickup point (%f, %f) does not match the address it was created at (%f, %f)",
					*o.PickupLat, *o.PickupLon, custLat, custLon)
			}
			if !o.CanAccept {
				t.Errorf("executor %.3f km away should be allowed to accept this order", o.DistanceKM)
			}
			break
		}
	}
	if !found {
		t.Errorf("created order %s was not found in map orders for executor", order.ID)
	}

	// Исполнитель в другом городе не должен видеть его вовсе: то же правило
	// радиуса, что кладёт заказ на эту карту, держит его вне чужих. Его
	// регистрация в этом потоке только для чтения — заказ он никогда не берёт.
	distantLat, distantLon := 59.9311, 30.3609 // Санкт-Петербург, ~630 км
	distant, err := authService.RegisterWithCoordinates(
		ctx, "+7999"+uuid.New().String()[:7], "far_"+uuid.New().String()[:8]+"@test.com",
		"Password123!", "Сидоров", "Сидор", "Сидорович",
		"1990-05-17",
		"Россия, г. Санкт-Петербург, Невский пр., д. 1", "EXECUTOR", &distantLat, &distantLon,
	)
	if err != nil {
		t.Fatalf("distant executor registration failed: %v", err)
	}
	distantOrders, err := executorGeoService.GetMapOrders(ctx, distant.ID)
	if err != nil {
		t.Fatalf("GetMapOrders for the distant executor failed: %v", err)
	}
	for _, o := range distantOrders {
		if o.ID == order.ID {
			t.Errorf("order %s must not be visible to an executor 630 km away", order.ID)
		}
	}

	// 6. Исполнитель принимает заказ
	err = orderService.Accept(ctx, order.ID, executor.ID)
	if err != nil {
		t.Fatalf("Executor failed to accept order: %v", err)
	}
	acceptedOrder, err := orderRepo.FindByID(ctx, order.ID)
	if err != nil || acceptedOrder == nil {
		t.Fatalf("failed to fetch accepted order: %v", err)
	}
	if acceptedOrder.Status != repository.OrderStatusAssigned {
		t.Errorf("expected accepted order status ASSIGNED, got %s", acceptedOrder.Status)
	}

	// 7. Исполнитель выполняет и подтверждает заказ
	err = orderService.ExecuteOrder(ctx, order.ID, executor.ID)
	if err != nil {
		t.Fatalf("Executor failed to execute order: %v", err)
	}
	err = orderService.ConfirmOrder(ctx, order.ID)
	if err != nil {
		t.Fatalf("Failed to confirm order: %v", err)
	}

	completedOrder, err := orderRepo.FindByID(ctx, order.ID)
	if err != nil || completedOrder == nil {
		t.Fatalf("failed to fetch completed order: %v", err)
	}
	if completedOrder.Status != repository.OrderStatusCompleted {
		t.Errorf("expected completed order status COMPLETED, got %s", completedOrder.Status)
	}

	// Проверяем, что баланс исполнителя пополнен
	execUser, err := userRepo.FindByID(ctx, executor.ID)
	if err != nil {
		t.Fatalf("failed to fetch executor user: %v", err)
	}
	if execUser.Balance <= 0 {
		t.Errorf("expected executor balance to be credited, got %s", execUser.Balance)
	}
}

// Автоматическое назначение ограничено той же географией, что и карта.
// Исполнитель в другом городе на смене, верифицирован и допущен, поэтому
// расстояние — единственное, что может удержать заказ вне его рук; ровно в этом
// воркер и ошибался, когда не мог прочитать позицию.
func TestE2E_MatchingDoesNotAssignAcrossTheCountry(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	userRepo := repository.New(db)
	orderRepo := repository.NewOrderRepository(db)
	shiftRepo := repository.NewShiftRepository(db)
	transactionRepo := repository.NewTransactionRepository(db)
	systemAccountRepo := repository.NewSystemAccountRepository(db)
	catalogRepo := repository.NewServiceCatalogRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	executorGeoRepo := repository.NewExecutorGeoRepository(db)
	addressRepo := repository.NewAddressRepository(db)

	ledger := service.NewLedger(transactionRepo, systemAccountRepo)
	authService := service.NewAuthServiceWithSecret(userRepo, "test-secret-key-12345", nil, nil).
		WithAddresses(addressRepo).
		WithExecutorGeo(executorGeoRepo)
	orderService := service.NewOrderService(orderRepo, ledger, settingsRepo, userRepo, shiftRepo, nil, catalogRepo, nil).
		WithExecutorGeo(executorGeoRepo)
	shiftService := service.NewShiftService(shiftRepo, ledger, settingsRepo, orderRepo, catalogRepo, db)
	matchingService := service.NewMatchingService(orderRepo, shiftRepo, userRepo, catalogRepo).
		WithGeo(executorGeoRepo, settingsRepo)

	variantID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO service_nodes (id, code, name, node_type, base_price, is_active)
		 VALUES ($1, $2, $3::jsonb, 'VARIANT', 100, true)`,
		variantID, "e2e-match-"+uuid.New().String()[:8],
		`{"ru": "Стандартный вывоз", "en": "Standard pickup"}`,
	); err != nil {
		t.Fatalf("failed to insert test service variant: %v", err)
	}

	// Заказчик в Москве с пополненной верифицированной учёткой.
	custLat, custLon := 55.7512, 37.6000
	customer, err := authService.RegisterWithCoordinates(
		ctx, "+7999"+uuid.New().String()[:7], "cust_"+uuid.New().String()[:8]+"@test.com",
		"Password123!", "Иванов", "Иван", "Иванович",
		"1990-05-17",
		"Россия, г. Москва, ул. Арбат, д. 10", "CUSTOMER", &custLat, &custLon,
	)
	if err != nil {
		t.Fatalf("customer registration failed: %v", err)
	}
	if err := userRepo.UpdateBalance(ctx, customer.ID, money.FromRubles(5000)); err != nil {
		t.Fatalf("failed to fund customer: %v", err)
	}
	if err := userRepo.UpdateVerified(ctx, customer.ID, true); err != nil {
		t.Fatalf("failed to verify customer: %v", err)
	}

	order, err := orderService.CreateOrder(ctx, customer.ID, variantID, false, false,
		"Россия, г. Москва, ул. Арбат, д. 10", &custLat, &custLon)
	if err != nil {
		t.Fatalf("order creation failed: %v", err)
	}

	// Дальний исполнитель обязан быть единственным кандидатом, иначе проверка ниже
	// ничего не значит, а тестовая база общая с прочими e2e-случаями, которые
	// оставляют исполнителей на смене после себя.
	if _, err := db.Exec(`UPDATE shifts SET status = 'COMPLETED' WHERE status = 'ACTIVE'`); err != nil {
		t.Fatalf("failed to close pre-existing shifts: %v", err)
	}

	// Единственный исполнитель на смене — в 630 км.
	distantLat, distantLon := 59.9311, 30.3609
	distant, err := authService.RegisterWithCoordinates(
		ctx, "+7999"+uuid.New().String()[:7], "far_"+uuid.New().String()[:8]+"@test.com",
		"Password123!", "Сидоров", "Сидор", "Сидорович",
		"1990-05-17",
		"Россия, г. Санкт-Петербург, Невский пр., д. 1", "EXECUTOR", &distantLat, &distantLon,
	)
	if err != nil {
		t.Fatalf("distant executor registration failed: %v", err)
	}
	if err := userRepo.UpdateVerified(ctx, distant.ID, true); err != nil {
		t.Fatalf("failed to verify executor: %v", err)
	}
	if err := userRepo.UpdateUserBirthDate(ctx, distant.ID, time.Now().AddDate(-25, 0, 0)); err != nil {
		t.Fatalf("failed to set executor birth date: %v", err)
	}
	if _, err := shiftService.StartShift(ctx, distant.ID, 3); err != nil {
		t.Fatalf("distant executor failed to start a shift: %v", err)
	}

	// Второй кандидат с неизвестной позицией: зарегистрирован без координат и без
	// разрешателя, поэтому для него ничего никогда не сохранялось.
	// Это тот случай, который раньше проскакивал: проверка расстояния
	// пропускалась всякий раз, когда позицию не удавалось прочитать, и заказ
	// уходил тому, кто просто оказался на смене.
	unlocated, err := authService.RegisterWithCoordinates(
		ctx, "+7999"+uuid.New().String()[:7], "nowhere_"+uuid.New().String()[:8]+"@test.com",
		"Password123!", "Незнамов", "Никита", "Никитич",
		"1990-05-17",
		"Россия, г. Москва, ул. Арбат, д. 10", "EXECUTOR", nil, nil,
	)
	if err != nil {
		t.Fatalf("unlocated executor registration failed: %v", err)
	}
	if err := userRepo.UpdateVerified(ctx, unlocated.ID, true); err != nil {
		t.Fatalf("failed to verify executor: %v", err)
	}
	if err := userRepo.UpdateUserBirthDate(ctx, unlocated.ID, time.Now().AddDate(-25, 0, 0)); err != nil {
		t.Fatalf("failed to set executor birth date: %v", err)
	}
	if _, err := shiftService.StartShift(ctx, unlocated.ID, 3); err != nil {
		t.Fatalf("unlocated executor failed to start a shift: %v", err)
	}
	lat, lon, _, err := executorGeoRepo.GetExecutorLocation(ctx, unlocated.ID)
	if err != nil {
		t.Fatalf("failed to read the unlocated executor's position: %v", err)
	}
	if lat != nil || lon != nil {
		t.Fatalf("this executor must have no stored position for the test to mean anything, got (%v, %v)", lat, lon)
	}

	// Автоподбор по умолчанию выключен; включаем его, чтобы этот тест проверял
	// фильтрацию по географии, а не проходил потому, что воркер ничего не сделал.
	if err := settingsRepo.UpdateSettings(ctx, map[string]string{"auto_matching_enabled": "1"}); err != nil {
		t.Fatalf("failed to enable auto matching: %v", err)
	}

	if err := matchingService.MatchOrders(ctx); err != nil {
		t.Fatalf("matching cycle failed: %v", err)
	}

	got, err := orderRepo.FindByID(ctx, order.ID)
	if err != nil || got == nil {
		t.Fatalf("failed to re-read the order: %v", err)
	}
	if got.ExecutorID != nil && *got.ExecutorID == distant.ID {
		t.Fatal("automatic matching assigned a Moscow order to an executor in Saint Petersburg")
	}
	if got.ExecutorID != nil && *got.ExecutorID == unlocated.ID {
		t.Fatal("automatic matching assigned the order to an executor whose position is unknown")
	}
	if got.Status != repository.OrderStatusSearching {
		t.Errorf("expected the order to stay SEARCHING, got %s", got.Status)
	}
}
