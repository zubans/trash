package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// Радиус взятия заказа охраняется сервером, а не кнопкой.
//
// Раньше он жил только на клиенте: карта считала can_accept и прятала кнопку, а
// OrderService.Accept координат не смотрел вовсе. Список «Заказы поблизости» на
// дашборде показывал кнопку безусловно на радиусе в 5 км, поэтому заказ вне
// круга брался обычным нажатием — с этого и начался разбор.
//
// Тест идёт тем же путём, что и приложение: карта сообщает can_accept, а затем
// вызывается Accept. Оба конца проверяются в одном тесте специально — если они
// разойдутся снова, упадёт именно эта проверка.
type acceptRadiusFixture struct {
	orderService *service.OrderService
	geoService   *service.ExecutorGeoService
	orderRepo    repository.OrderRepository
	customerID   uuid.UUID
	executorID   uuid.UUID
	orderID      uuid.UUID
	distanceKM   float64
}

// setupAcceptRadius поднимает заказчика с заказом и исполнителя на смене,
// разнесённых на deltaLat градусов широты (~111 км на градус).
func setupAcceptRadius(t *testing.T, deltaLat float64) *acceptRadiusFixture {
	t.Helper()
	db := setupTestDB(t)
	t.Cleanup(func() { db.Close() })

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
	geoService := service.NewExecutorGeoService(executorGeoRepo, orderRepo).
		WithEligibility(userRepo, settingsRepo, catalogRepo)

	variantID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO service_nodes (id, code, name, node_type, base_price, is_active)
		 VALUES ($1, $2, $3::jsonb, 'VARIANT', 100, true)`,
		variantID,
		"radius-variant-"+uuid.New().String()[:8],
		`{"ru": "Стандартный вывоз", "en": "Standard pickup"}`,
	); err != nil {
		t.Fatalf("insert variant: %v", err)
	}

	custLat, custLon := 55.7512, 37.6000
	customer, err := authService.RegisterWithCoordinates(
		ctx, "+7999"+uuid.New().String()[:7], "cust_"+uuid.New().String()[:8]+"@test.com",
		"Password123!", "Иванов", "Иван", "Иванович", "1990-05-17",
		"Россия, г. Москва, ул. Арбат, д. 10", "CUSTOMER", &custLat, &custLon,
	)
	if err != nil {
		t.Fatalf("customer registration: %v", err)
	}
	_ = userRepo.UpdateBalance(ctx, customer.ID, money.FromRubles(5000))
	_ = userRepo.UpdateVerified(ctx, customer.ID, true)

	order, err := orderService.CreateOrder(
		ctx, customer.ID, variantID, false, false,
		"Россия, г. Москва, ул. Арбат, д. 10", &custLat, &custLon,
	)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	execLat, execLon := custLat+deltaLat, custLon
	executor, err := authService.RegisterWithCoordinates(
		ctx, "+7999"+uuid.New().String()[:7], "exec_"+uuid.New().String()[:8]+"@test.com",
		"Password123!", "Петров", "Пётр", "Петрович", "1990-05-17",
		"Россия, г. Москва, ул. Новый Арбат, д. 2", "EXECUTOR", &execLat, &execLon,
	)
	if err != nil {
		t.Fatalf("executor registration: %v", err)
	}
	_ = userRepo.UpdateVerified(ctx, executor.ID, true)
	_ = userRepo.UpdateUserBirthDate(ctx, executor.ID, time.Now().AddDate(-25, 0, 0))
	_ = userRepo.UpdateBalance(ctx, executor.ID, money.FromRubles(5000))

	if _, err := shiftRepo.StartShift(ctx, executor.ID, 3); err != nil {
		t.Fatalf("start shift: %v", err)
	}

	return &acceptRadiusFixture{
		orderService: orderService,
		geoService:   geoService,
		orderRepo:    orderRepo,
		customerID:   customer.ID,
		executorID:   executor.ID,
		orderID:      order.ID,
		distanceKM:   service.HaversineDistanceKM(custLat, custLon, execLat, execLon),
	}
}

// Заказ за пределами круга сервер не отдаёт, даже если запрос пришёл в обход
// интерфейса. Это и есть та дыра, ради которой писался тест.
func TestAcceptRejectedOutsideRadius(t *testing.T) {
	f := setupAcceptRadius(t, 0.045) // ~5 км
	ctx := context.Background()

	if f.distanceKM < 4 || f.distanceKM > 6 {
		t.Fatalf("тест рассчитан на ~5 км, получилось %.2f км", f.distanceKM)
	}

	// Карта честно говорит, что взять нельзя.
	mapOrders, err := f.geoService.GetMapOrders(ctx, f.executorID)
	if err != nil {
		t.Fatalf("map orders: %v", err)
	}
	var seen bool
	for _, o := range mapOrders {
		if o.ID == f.orderID {
			seen = true
			if o.CanAccept {
				t.Fatalf("карта считает заказ в %.2f км доступным — тест построен неверно", o.DistanceKM)
			}
		}
	}
	if !seen {
		t.Fatal("заказ не попал на карту, тест построен неверно")
	}

	// И сервер говорит то же самое.
	err = f.orderService.Accept(ctx, f.orderID, f.executorID)
	if err == nil {
		t.Fatal("сервер принял заказ вне зоны взятия: проверка радиуса не работает")
	}
	if !strings.Contains(err.Error(), "вне зоны взятия") {
		t.Fatalf("ожидался отказ по радиусу, получено: %v", err)
	}

	// Заказ остался свободным: отказ не должен ничего назначать.
	updated, err := f.orderRepo.GetOrderByID(ctx, f.orderID)
	if err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if updated.Status != repository.OrderStatusSearching || updated.ExecutorID != nil {
		t.Fatalf("после отказа заказ должен остаться в поиске, получено %s", updated.Status)
	}
}

// Внутри круга всё работает как прежде: проверка не должна закрыть основной
// сценарий заодно с дырой.
func TestAcceptAllowedInsideRadius(t *testing.T) {
	f := setupAcceptRadius(t, 0.0015) // ~170 м
	ctx := context.Background()

	if f.distanceKM > 0.5 {
		t.Fatalf("тест рассчитан на точку внутри круга, получилось %.2f км", f.distanceKM)
	}

	mapOrders, err := f.geoService.GetMapOrders(ctx, f.executorID)
	if err != nil {
		t.Fatalf("map orders: %v", err)
	}
	for _, o := range mapOrders {
		if o.ID == f.orderID && !o.CanAccept {
			t.Fatalf("карта не даёт взять заказ в %.2f км, хотя он внутри круга", o.DistanceKM)
		}
	}

	if err := f.orderService.Accept(ctx, f.orderID, f.executorID); err != nil {
		t.Fatalf("заказ внутри круга должен браться, получено: %v", err)
	}

	updated, err := f.orderRepo.GetOrderByID(ctx, f.orderID)
	if err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if updated.Status != repository.OrderStatusAssigned || updated.ExecutorID == nil || *updated.ExecutorID != f.executorID {
		t.Fatalf("заказ не назначился: статус %s", updated.Status)
	}
}

// Радиус берётся из настройки админки, а не из константы: расширив её, площадку
// можно открыть на весь город, не пересобирая образ.
func TestAcceptRadiusFollowsSetting(t *testing.T) {
	f := setupAcceptRadius(t, 0.045) // ~5 км, вне умолчания в 0.5 км
	ctx := context.Background()

	db := setupTestDB(t)
	// Закрытие регистрируется ДО сброса настройки: t.Cleanup выполняется в
	// обратном порядке, поэтому настройка вернётся на живом соединении, а
	// закроется оно уже после. С `defer db.Close()` соединение закрывалось
	// раньше, сброс молча не проходил, и 10 км оставались в общей базе, роняя
	// соседние тесты на следующем прогоне.
	t.Cleanup(func() { db.Close() })

	settingsRepo := repository.NewSettingsRepository(db)
	if err := settingsRepo.UpdateSettings(ctx, map[string]string{service.SettingAcceptRadiusKM: "10"}); err != nil {
		t.Fatalf("update setting: %v", err)
	}
	// Возврат к посеянному миграцией значению, а не к пустой строке или нулю:
	// пустое значение UpdateSettings пропускает, а ноль запрещён (он читался бы
	// как «не задано» и расходился бы с тем, что показано в поле). Без возврата
	// 10 км осели бы в общей базе и уронили соседние тесты на следующем прогоне.
	t.Cleanup(func() {
		if err := settingsRepo.UpdateSettings(ctx, map[string]string{service.SettingAcceptRadiusKM: "0.5"}); err != nil {
			t.Errorf("не удалось вернуть %s: %v", service.SettingAcceptRadiusKM, err)
		}
	})

	if err := f.orderService.Accept(ctx, f.orderID, f.executorID); err != nil {
		t.Fatalf("с радиусом 10 км заказ в %.2f км должен браться, получено: %v", f.distanceKM, err)
	}
}

// Радиусы в админке обязаны быть положительными: ноль читался бы кодом как «не
// задано», поле показывало бы 0, а действовало бы умолчание. Настройка, которой
// нельзя верить на слово, хуже отсутствующей.
func TestRadiusSettingsRejectZero(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	adminService := service.NewAdminService(
		repository.New(db),
		repository.NewAdminRepository(db),
		repository.NewSettingsRepository(db),
		"test-secret-key-12345",
		nil,
	)

	for _, key := range []string{service.SettingAcceptRadiusKM, service.SettingMapOverviewRadiusKM} {
		if err := adminService.UpdateSettings(ctx, map[string]string{key: "0"}); err == nil {
			t.Fatalf("нулевой %s должен быть отвергнут", key)
		}
		if err := adminService.UpdateSettings(ctx, map[string]string{key: "-1"}); err == nil {
			t.Fatalf("отрицательный %s должен быть отвергнут", key)
		}
	}

	// Обзор сверху ограничен: круг в тысячу километров превратил бы экран,
	// открытый у каждого исполнителя, в чтение всей таблицы заказов.
	if err := adminService.UpdateSettings(ctx, map[string]string{service.SettingMapOverviewRadiusKM: "500"}); err == nil {
		t.Fatal("обзор в 500 км должен быть отвергнут")
	}
}
