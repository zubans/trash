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

	// Repositories
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

	// Wired exactly as main.go does it: shift location reports are written
	// through the geo service, which is what makes the position the map and
	// matching read the same one the executor's app reported.
	shiftService := service.NewShiftService(shiftRepo, ledger, settingsRepo, orderRepo, catalogRepo, db).
		WithExecutorLocation(executorGeoService)

	// A priced, active variant to order. The catalog stores names as JSONB and
	// requires a price on a VARIANT, so the row is built to satisfy both.
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

	// 1. Register Customer with Address
	custPhone := "+7999" + uuid.New().String()[:7]
	custEmail := "cust_" + uuid.New().String()[:8] + "@test.com"
	custAddress := "Россия, г. Москва, ул. Арбат, д. 10"
	custLat, custLon := 55.7512, 37.6000

	customer, err := authService.RegisterWithCoordinates(
		ctx, custPhone, custEmail, "Password123!",
		"Иванов", "Иван", "Иванович",
		custAddress, "CUSTOMER", &custLat, &custLon,
	)
	if err != nil {
		t.Fatalf("Customer registration failed: %v", err)
	}

	// Verify Customer address is in unified `addresses` table
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

	// Top up customer balance so hold succeeds
	_ = userRepo.UpdateBalance(ctx, customer.ID, money.FromRubles(5000))
	_ = userRepo.UpdateVerified(ctx, customer.ID, true)

	// 2. Customer creates an order using their address
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

	// 3. Register Executor with Address in proximity (~150 meters away)
	execPhone := "+7999" + uuid.New().String()[:7]
	execEmail := "exec_" + uuid.New().String()[:8] + "@test.com"
	execAddress := "Россия, г. Москва, ул. Новый Арбат, д. 2"
	execLat, execLon := 55.7520, 37.6010

	executor, err := authService.RegisterWithCoordinates(
		ctx, execPhone, execEmail, "Password123!",
		"Петров", "Петр", "Петрович",
		execAddress, "EXECUTOR", &execLat, &execLon,
	)
	if err != nil {
		t.Fatalf("Executor registration failed: %v", err)
	}

	// Verify Executor address is in unified `addresses` table
	execAddrs, err := addressRepo.List(ctx, executor.ID)
	if err != nil || len(execAddrs) == 0 {
		t.Fatalf("Executor address was not saved in addresses table: %v", err)
	}
	_ = userRepo.UpdateVerified(ctx, executor.ID, true)
	birthDate := time.Now().AddDate(-25, 0, 0)
	_ = userRepo.UpdateUserBirthDate(ctx, executor.ID, birthDate)

	// 4. Executor starts active shift
	shift, err := shiftService.StartShift(ctx, executor.ID, 3)
	if err != nil {
		t.Fatalf("Executor start shift failed: %v", err)
	}
	if shift.Status != repository.ShiftStatusActive {
		t.Errorf("expected shift status ACTIVE, got %s", shift.Status)
	}

	// The executor's app reports its position through the shift endpoint. This
	// is the path that used to accept coordinates and discard them, so the test
	// goes through it rather than writing to the repository directly.
	//
	// The reported point is deliberately NOT the one registration stored: if the
	// report were dropped, the position would still read back as the
	// registration fix and the check would pass while the feature was broken.
	// The move is ~55 m, inside the accept radius, so it is an ordinary
	// position update rather than a district change.
	movedLat, movedLon := execLat+0.0005, execLon
	stored, err := shiftService.RecordLocation(ctx, executor.ID, movedLat, movedLon)
	if err != nil {
		t.Fatalf("failed to record executor location: %v", err)
	}
	if !stored {
		t.Fatal("the reported position was not stored")
	}

	// It must read back as the executor's authoritative position — the reported
	// point, not the one registration left behind.
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
	// From here on the executor is at the position they reported.
	execLat, execLon = movedLat, movedLon

	// 5. Executor queries available orders on map
	mapOrders, err := executorGeoService.GetMapOrders(ctx, executor.ID)
	if err != nil {
		t.Fatalf("GetMapOrders failed: %v", err)
	}
	if len(mapOrders) == 0 {
		t.Fatalf("expected to find customer's order on the map, got 0 orders")
	}

	// The order must appear at the distance the two coordinate pairs actually
	// imply — that is what "sees it in the right place" means.
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

	// An executor in another city must not see it at all: the same radius rule
	// that puts the order on this map keeps it off everyone else's. Registering
	// them is read-only for this flow — they never take the order.
	distantLat, distantLon := 59.9311, 30.3609 // Saint Petersburg, ~630 km away
	distant, err := authService.RegisterWithCoordinates(
		ctx, "+7999"+uuid.New().String()[:7], "far_"+uuid.New().String()[:8]+"@test.com",
		"Password123!", "Сидоров", "Сидор", "Сидорович",
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

	// 6. Executor accepts the order
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

	// 7. Executor executes and confirms the order
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

	// Verify executor balance was credited
	execUser, err := userRepo.FindByID(ctx, executor.ID)
	if err != nil {
		t.Fatalf("failed to fetch executor user: %v", err)
	}
	if execUser.Balance <= 0 {
		t.Errorf("expected executor balance to be credited, got %s", execUser.Balance)
	}
}

// Automatic assignment is bounded by the same geography as the map. An executor
// in another city is on shift, verified and eligible, so distance is the only
// thing that can keep the order out of their hands — which is exactly what the
// worker used to get wrong when it could not read a position.
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

	// A customer in Moscow with a funded, verified account.
	custLat, custLon := 55.7512, 37.6000
	customer, err := authService.RegisterWithCoordinates(
		ctx, "+7999"+uuid.New().String()[:7], "cust_"+uuid.New().String()[:8]+"@test.com",
		"Password123!", "Иванов", "Иван", "Иванович",
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

	// The distant executor has to be the only candidate for the assertion below
	// to mean anything, and the test database is shared with the other e2e
	// cases, which leave executors on shift behind them.
	if _, err := db.Exec(`UPDATE shifts SET status = 'COMPLETED' WHERE status = 'ACTIVE'`); err != nil {
		t.Fatalf("failed to close pre-existing shifts: %v", err)
	}

	// The only executor on shift is 630 km away.
	distantLat, distantLon := 59.9311, 30.3609
	distant, err := authService.RegisterWithCoordinates(
		ctx, "+7999"+uuid.New().String()[:7], "far_"+uuid.New().String()[:8]+"@test.com",
		"Password123!", "Сидоров", "Сидор", "Сидорович",
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

	// A second candidate whose position is unknown: registered without
	// coordinates and with no resolver, so nothing was ever stored for them.
	// This is the case that used to slip through — the distance check was
	// skipped whenever it could not read a position, and the order went out to
	// whoever happened to be on shift.
	unlocated, err := authService.RegisterWithCoordinates(
		ctx, "+7999"+uuid.New().String()[:7], "nowhere_"+uuid.New().String()[:8]+"@test.com",
		"Password123!", "Незнамов", "Никита", "Никитич",
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

	// Automatic matching is off by default; turn it on so this test exercises the
	// geography filtering rather than passing because the worker did nothing.
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
