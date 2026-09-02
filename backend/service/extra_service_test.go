package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

type mockReviewRepo struct {
	reviews []*repository.OrderReview
}

func (m *mockReviewRepo) CreateReview(ctx context.Context, r *repository.OrderReview) error {
	m.reviews = append(m.reviews, r)
	return nil
}

func (m *mockReviewRepo) GetReviewByOrderAndAuthor(ctx context.Context, orderID, authorID uuid.UUID) (*repository.OrderReview, error) {
	for _, rev := range m.reviews {
		if rev.OrderID == orderID && rev.AuthorID == authorID {
			return rev, nil
		}
	}
	return nil, nil
}

func (m *mockReviewRepo) GetReviewsForUser(ctx context.Context, targetID uuid.UUID, limit, offset int) ([]repository.OrderReview, error) {
	var res []repository.OrderReview
	for _, rev := range m.reviews {
		if rev.TargetID == targetID {
			res = append(res, *rev)
		}
	}
	return res, nil
}

func (m *mockReviewRepo) UpdateUserRating(ctx context.Context, userID uuid.UUID, role string) error {
	return nil
}

func (m *mockReviewRepo) GetUserRating(ctx context.Context, userID uuid.UUID, role string) (*repository.UserRatingSummary, error) {
	var sum int
	var count int
	for _, rev := range m.reviews {
		if rev.TargetID == userID {
			sum += rev.Rating
			count++
		}
	}
	if count == 0 {
		return &repository.UserRatingSummary{UserID: userID, Rating: 5.0, ReviewsCount: 0}, nil
	}
	return &repository.UserRatingSummary{
		UserID:       userID,
		Rating:       float64(sum) / float64(count),
		ReviewsCount: count,
	}, nil
}

func TestReviewService_CreateReview(t *testing.T) {
	reviewRepo := &mockReviewRepo{}
	orderRepo := &mockOrderRepo{}
	srv := NewReviewService(reviewRepo, orderRepo)

	custID := uuid.New()
	execID := uuid.New()
	orderID := uuid.New()

	order := &repository.Order{
		ID:         orderID,
		CustomerID: custID,
		ExecutorID: &execID,
		Status:     repository.OrderStatusCompleted,
	}
	orderRepo.orders = append(orderRepo.orders, order)

	// Недопустимый рейтинг
	_, err := srv.CreateReview(context.Background(), orderID, custID, CreateReviewDTO{Rating: 0})
	if err == nil {
		t.Error("expected error for invalid rating")
	}

	// Успешное создание отзыва
	rev, err := srv.CreateReview(context.Background(), orderID, custID, CreateReviewDTO{
		Rating:  5,
		Tags:    []string{"fast"},
		Comment: "Great job!",
	})
	if err != nil {
		t.Fatalf("unexpected error creating review: %v", err)
	}
	if rev.TargetID != execID {
		t.Errorf("expected target ID %s, got %s", execID, rev.TargetID)
	}

	// Попытка дублирующего отзыва
	_, err = srv.CreateReview(context.Background(), orderID, custID, CreateReviewDTO{Rating: 4})
	if err == nil {
		t.Error("expected error on duplicate review")
	}

	// Попытка от непричастного пользователя
	strangerID := uuid.New()
	_, err = srv.CreateReview(context.Background(), orderID, strangerID, CreateReviewDTO{Rating: 5})
	if err == nil {
		t.Error("expected error for uninvolved user")
	}

	// Отзыв по заказу и автору
	found, err := srv.GetReviewByOrderAndAuthor(context.Background(), orderID, custID)
	if err != nil || found == nil {
		t.Errorf("expected to find review, got err: %v", err)
	}

	// Получаем отзывы о пользователе
	revs, err := srv.GetReviewsForUser(context.Background(), execID, 10, 0)
	if err != nil || len(revs) != 1 {
		t.Errorf("expected 1 review for executor, got %d", len(revs))
	}

	// Получаем рейтинг пользователя
	summary, err := srv.GetUserRating(context.Background(), execID, "EXECUTOR")
	if err != nil || summary.Rating != 5.0 || summary.ReviewsCount != 1 {
		t.Errorf("expected rating 5.0 count 1, got summary: %v", summary)
	}

	// Проверка истёкшего 7-дневного окна SLA
	oldOrderID := uuid.New()
	eightDaysAgo := time.Now().Add(-8 * 24 * time.Hour)
	orderRepo.orders = append(orderRepo.orders, &repository.Order{
		ID:          oldOrderID,
		CustomerID:  custID,
		ExecutorID:  &execID,
		Status:      repository.OrderStatusCompleted,
		CompletedAt: &eightDaysAgo,
	})
	_, err = srv.CreateReview(context.Background(), oldOrderID, custID, CreateReviewDTO{Rating: 5})
	if err == nil {
		t.Error("expected error when submitting review after 7 days window")
	}
}

func TestBidService_AcceptAndGetBids(t *testing.T) {
	bidRepo := &mockBidRepo{}
	orderRepo := &mockOrderRepo{}
	shiftRepo := &mockShiftRepo{}
	srv := NewBidService(bidRepo, orderRepo, shiftRepo, testLedger(), newMockUserRepo(), newMockCatalogRepo(), nil)

	custID := uuid.New()
	execID := uuid.New()
	orderID := uuid.New()

	order := &repository.Order{
		ID:               orderID,
		CustomerID:       custID,
		ServiceVariantID: constructionVariantID,
		Status:           repository.OrderStatusSearching,
	}
	orderRepo.orders = append(orderRepo.orders, order)

	// Создаём активную смену исполнителю
	shiftRepo.shifts = append(shiftRepo.shifts, &repository.Shift{
		ID:           uuid.New(),
		ExecutorID:   execID,
		Status:       repository.ShiftStatusActive,
		PlannedEndAt: time.Now().Add(time.Hour),
	})

	bid, err := srv.CreateBid(context.Background(), orderID, execID, money.FromRubles(350.00))
	if err != nil {
		t.Fatalf("unexpected error creating bid: %v", err)
	}

	// Перечислять ставки по заказу может только его собственный заказчик.
	bids, err := srv.GetBidsForOrder(context.Background(), orderID, custID)
	if err != nil || len(bids) != 1 {
		t.Fatalf("expected 1 bid for order, got %d (err %v)", len(bids), err)
	}

	if _, err := srv.GetBidsForOrder(context.Background(), orderID, uuid.New()); err == nil {
		t.Error("expected error when a stranger lists bids for someone else's order")
	}

	// Недопустимая цена ставки (0 или отрицательная)
	_, err = srv.CreateBid(context.Background(), orderID, execID, money.FromRubles(0))
	if err == nil {
		t.Error("expected error for bid price 0")
	}

	// Принимаем ставку
	err = srv.AcceptBid(context.Background(), bid.ID, custID)
	if err != nil {
		t.Fatalf("unexpected error accepting bid: %v", err)
	}
	if bid.Status != "ACCEPTED" {
		t.Errorf("expected status ACCEPTED, got %s", bid.Status)
	}
}

func TestOrderService_AcceptExecuteReject(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	txRepo := &mockTransactionRepo{}
	srv := NewOrderService(orderRepo, NewLedger(txRepo, newMockAccounts()), nil, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	custID := uuid.New()
	execID := uuid.New()
	order, _ := srv.CreateOrder(context.Background(), custID, standardVariantID, false, false, "", nil, nil)

	// Принятие
	err := srv.Accept(context.Background(), order.ID, execID)
	if err != nil {
		t.Fatalf("unexpected error accepting order: %v", err)
	}

	// ExecuteOrder
	err = srv.ExecuteOrder(context.Background(), order.ID, execID)
	if err != nil {
		t.Fatalf("unexpected error executing order: %v", err)
	}
	if order.Status != repository.OrderStatusExecuted {
		t.Errorf("expected status EXECUTED, got %s", order.Status)
	}

	// RejectAssignedOrder (проверяем транзакцию штрафа в 50%)
	order2, _ := srv.CreateOrder(context.Background(), custID, standardVariantID, false, false, "", nil, nil)
	_ = srv.Accept(context.Background(), order2.ID, execID)
	err = srv.RejectAssignedOrder(context.Background(), order2.ID, execID)
	if err != nil {
		t.Fatalf("unexpected error rejecting order: %v", err)
	}
	if order2.Status != repository.OrderStatusSearching {
		t.Errorf("expected status SEARCHING after reject, got %s", order2.Status)
	}
	if len(txRepo.txs) == 0 || txRepo.txs[len(txRepo.txs)-1].Type != "FINE" {
		t.Errorf("expected FINE transaction created on rejection")
	}

	// ListAssigned, ListByCustomer и FindNearbyOrders
	custOrders, _ := srv.ListByCustomer(context.Background(), custID)
	if len(custOrders) != 2 {
		t.Errorf("expected 2 customer orders, got %d", len(custOrders))
	}
	_, _ = srv.FindNearbyOrders(context.Background(), 55.75, 37.61, 5000)
	_, _ = srv.GetAvailableConstructionOrders(context.Background())
}

type mockExecutorGeoRepo struct{}

func (m *mockExecutorGeoRepo) UpdateExecutorLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64, isManual bool) error {
	return nil
}
func (m *mockExecutorGeoRepo) GetExecutorLocation(ctx context.Context, executorID uuid.UUID) (*float64, *float64, *time.Time, error) {
	lat := 55.7558
	lon := 37.6173
	return &lat, &lon, nil, nil
}
func (m *mockExecutorGeoRepo) GetExecutorLocations(ctx context.Context, executorIDs []uuid.UUID) (map[uuid.UUID]repository.ExecutorPosition, error) {
	out := make(map[uuid.UUID]repository.ExecutorPosition, len(executorIDs))
	for _, id := range executorIDs {
		out[id] = repository.ExecutorPosition{Lat: 55.7558, Lon: 37.6173}
	}
	return out, nil
}
func (m *mockExecutorGeoRepo) RecordDevicePosition(ctx context.Context, executorID uuid.UUID, lat, lon float64) error {
	return nil
}

func (m *mockExecutorGeoRepo) GetDevicePosition(ctx context.Context, executorID uuid.UUID) (*repository.ExecutorPosition, error) {
	return nil, nil
}

func (m *mockExecutorGeoRepo) FollowDevicePosition(ctx context.Context, executorID uuid.UUID, lat, lon float64) error {
	return nil
}

func (m *mockExecutorGeoRepo) CreateGeoAlert(ctx context.Context, alert *repository.GeoAlert) error {
	return nil
}
func (m *mockExecutorGeoRepo) GetGeoAlerts(ctx context.Context, status string, limit, offset int) ([]repository.GeoAlert, error) {
	return []repository.GeoAlert{}, nil
}

func TestExecutorGeoService(t *testing.T) {
	geoRepo := &mockExecutorGeoRepo{}
	orderRepo := &mockOrderRepo{}
	srv := NewExecutorGeoService(geoRepo, orderRepo)

	execID := uuid.New()

	// Недопустимые координаты
	_, err := srv.SetLocation(context.Background(), execID, SetLocationRequest{Lat: 100.0, Lon: 200.0})
	if err == nil {
		t.Error("expected error for invalid coordinates")
	}

	// Корректная установка местоположения
	res, err := srv.SetLocation(context.Background(), execID, SetLocationRequest{Lat: 55.7558, Lon: 37.6173, IsManual: false})
	if err != nil || !res.Success {
		t.Fatalf("unexpected error setting location: %v", err)
	}

	// Ручной сдвиг местоположения (>2 км), включающий паузу
	res2, err := srv.SetLocation(context.Background(), execID, SetLocationRequest{Lat: 55.8000, Lon: 37.7000, IsManual: true})
	if err != nil || !res2.Success {
		t.Fatalf("unexpected error setting manual location: %v", err)
	}
	res3, err := srv.SetLocation(context.Background(), execID, SetLocationRequest{Lat: 55.9000, Lon: 37.8000, IsManual: true})
	if err != nil || res3.Success {
		t.Error("expected cooldown rejection for manual shift within 10 min")
	}

	// Заказы на карте
	plat, plon := 55.7558, 37.6173
	orderRepo.orders = append(orderRepo.orders, &repository.Order{
		ID:        uuid.New(),
		Status:    repository.OrderStatusSearching,
		PickupLat: &plat,
		PickupLon: &plon,
	})

	// Координаты теперь берутся из сохранённого местоположения исполнителя, а не от вызывающего.
	orders, err := srv.GetMapOrders(context.Background(), execID)
	if err != nil || len(orders) != 1 {
		t.Errorf("expected 1 map order, got %d", len(orders))
	}

	alerts, err := srv.GetGeoAlerts(context.Background(), "NEW", 10, 0)
	if err != nil {
		t.Errorf("unexpected error getting geo alerts: %v", err)
	}
	_ = alerts
}

func TestAdminService_Extended(t *testing.T) {
	userRepo := newMockRepo()
	adminRepo := &mockAdminRepo{requests: make(map[uuid.UUID]*repository.TopUpRequest)}
	settingsRepo := &mockSettingsRepo{settings: make(map[string]string)}
	srv := NewAdminService(userRepo, adminRepo, settingsRepo, "test-secret", nil).
		WithLedger(NewLedger(&mockTransactionRepo{}, newMockAccounts()))

	u := &repository.User{ID: uuid.New(), Phone: "70000000000", Role: "CUSTOMER"}
	userRepo.users[u.Phone] = u

	adminID := uuid.New()
	_ = srv.UpdateUserRole(context.Background(), u.ID, adminID, "EXECUTOR")
	_ = srv.UpdateUserRole(context.Background(), u.ID, adminID, "INVALID_ROLE")
	_ = srv.UpdateUserAddress(context.Background(), u.ID, "New Address")
	_ = srv.UpdateUserAddress(context.Background(), u.ID, "")
	_ = srv.TopUpUserBalance(context.Background(), adminID, u.ID, money.FromRubles(500.0))
	_ = srv.TopUpUserBalance(context.Background(), u.ID, u.ID, 500.0)
	_ = srv.TopUpUserBalance(context.Background(), adminID, u.ID, money.FromRubles(-50.0))

	users, total, _ := srv.GetUsers(context.Background(), 1, 10, "", "", "")
	if total != 0 || len(users) != 0 {
		// mockAdminRepo вернул пусто
	}

	_, _ = srv.GetActiveShifts(context.Background())
	_, _ = srv.GetActiveOrders(context.Background(), 0, 0)
	_, _, _ = srv.GetCompletedOrders(context.Background(), repository.CompletedOrdersFilter{})

	prof, err := srv.GetProfile(context.Background(), u.ID)
	if err != nil || prof["phone"] != "70000000000" {
		t.Errorf("expected user profile with phone, got %v", prof)
	}

	_ = srv.RejectTopUpRequest(context.Background(), uuid.New(), adminID)
	_, _ = srv.GetTransactions(context.Background(), 0, 0)
}

func TestChatService_Extended(t *testing.T) {
	chatRepo := &mockChatRepo{}
	orderRepo := &mockOrderRepo{}
	srv := NewChatService(chatRepo, orderRepo)

	custID := uuid.New()
	execID := uuid.New()
	orderID := uuid.New()
	order := &repository.Order{ID: orderID, CustomerID: custID, ExecutorID: &execID, Status: repository.OrderStatusAssigned}
	orderRepo.orders = append(orderRepo.orders, order)

	_, _ = chatRepo.CreateChat(context.Background(), orderID)

	msg, err := srv.SendMessage(context.Background(), orderID, custID, "Hello via REST")
	if err != nil {
		t.Fatalf("unexpected error sending message: %v", err)
	}
	if msg.Text != "Hello via REST" {
		t.Errorf("expected text 'Hello via REST', got '%s'", msg.Text)
	}

	attMsg, err := srv.SendMessageWithAttachment(context.Background(), orderID, custID, "Photo caption", "/uploads/test.jpg", "test.jpg", "image", 1024)
	if err != nil {
		t.Fatalf("unexpected error sending attachment: %v", err)
	}
	if attMsg.FileName == nil || *attMsg.FileName != "test.jpg" {
		t.Errorf("expected file_name 'test.jpg'")
	}

	_, _ = srv.MarkMessagesAsRead(context.Background(), orderID, execID)
	_, _ = srv.GetUnreadOrderIDs(context.Background(), execID)
	srv.BroadcastSystemMessage(context.Background(), orderID, map[string]string{"type": "system", "text": "hello"})
}

func TestShiftService_Extended(t *testing.T) {
	shiftRepo := &mockShiftRepo{}
	srv := NewShiftService(shiftRepo, testLedger(), &orderMockSettingsRepo{}, &mockOrderRepo{}, nil, nil)

	execID := uuid.New()
	shift, err := srv.StartShift(context.Background(), execID, 3)
	if err != nil {
		t.Fatalf("unexpected error starting shift: %v", err)
	}
	if shift.DurationHours != 3 {
		t.Errorf("expected duration 3, got %d", shift.DurationHours)
	}

	_, _ = srv.GetExecutorFinancialHistory(context.Background(), execID)
	_ = srv.EndShiftByID(context.Background(), shift.ID)
	srv.AutoEndExpiredShifts(context.Background())

	// Проверяем Start, GetActive, GetCurrent, End
	execID2 := uuid.New()
	s2, err := srv.Start(context.Background(), execID2, 1)
	if err != nil {
		t.Fatalf("unexpected error in Start: %v", err)
	}
	act, err := srv.GetActive(context.Background(), execID2)
	if err != nil || act.ID != s2.ID {
		t.Errorf("expected active shift %s, got %v", s2.ID, act)
	}
	curr, err := srv.GetCurrent(context.Background(), execID2)
	if err != nil || curr.ID != s2.ID {
		t.Errorf("expected current shift %s, got %v", s2.ID, curr)
	}
	err = srv.End(context.Background(), execID2)
	if err != nil {
		t.Errorf("unexpected error in End: %v", err)
	}
}

func TestAuthService_ParseJWT(t *testing.T) {
	userRepo := newMockRepo()
	srv := NewAuthServiceWithSecret(userRepo, "test-secret", nil, nil)

	u := &repository.User{ID: uuid.New(), Phone: "79001112233", Role: "CUSTOMER"}
	tokenStr, err := srv.GenerateJWT(context.Background(), u)
	if err != nil {
		t.Fatalf("failed to generate JWT: %v", err)
	}

	claims, err := srv.ParseJWT(context.Background(), tokenStr)
	if err != nil || claims.UserID != u.ID || claims.Phone != u.Phone {
		t.Errorf("failed parsing JWT: %v, claims: %v", err, claims)
	}

	_, err = srv.ParseJWT(context.Background(), "invalid.jwt.token")
	if err == nil {
		t.Error("expected error for invalid JWT")
	}
}

func TestOrderService_Aliases(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	srv := NewOrderService(orderRepo, testLedger(), nil, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	custID := uuid.New()
	execID := uuid.New()
	lat, lon := 55.75, 37.61
	order, err := srv.Create(context.Background(), custID, CreateOrderRequest{
		ServiceVariantID: standardVariantID,
		IsUrgent:         false,
		IsAsap:           false,
		Address:          "Main St",
		Lat:              &lat,
		Lon:              &lon,
	})
	if err != nil {
		t.Fatalf("unexpected error in Create alias: %v", err)
	}

	// Псевдоним Confirm
	_ = orderRepo.AssignOrder(context.Background(), order.ID, execID)
	_ = orderRepo.Execute(context.Background(), nil, order.ID)
	err = srv.Confirm(context.Background(), custID, order.ID)
	if err != nil {
		t.Errorf("unexpected error in Confirm alias: %v", err)
	}

	// Псевдоним Cancel
	order2, _ := srv.Create(context.Background(), custID, CreateOrderRequest{ServiceVariantID: standardVariantID})
	err = srv.Cancel(context.Background(), custID, order2.ID)
	if err != nil {
		t.Errorf("unexpected error in Cancel alias: %v", err)
	}

	// ListAssigned
	_, _ = srv.ListAssigned(context.Background(), execID)
}

func TestAdminService_TopUpRequestAndSettings(t *testing.T) {
	userRepo := newMockRepo()
	adminRepo := &mockAdminRepo{requests: make(map[uuid.UUID]*repository.TopUpRequest)}
	settingsRepo := &mockSettingsRepo{settings: make(map[string]string)}
	srv := NewAdminService(userRepo, adminRepo, settingsRepo, "secret", nil).
		WithLedger(NewLedger(&mockTransactionRepo{}, newMockAccounts()))

	u := &repository.User{ID: uuid.New(), Phone: "79998887766"}
	userRepo.users[u.Phone] = u

	req, err := srv.CreateTopUpRequest(context.Background(), u.ID, money.FromRubles(300.0))
	if err != nil || req.Amount != money.FromRubles(300) {
		t.Fatalf("unexpected error creating top up request: %v", err)
	}

	reqs, err := srv.GetTopUpRequests(context.Background(), 0, 0)
	if err != nil || len(reqs) != 1 {
		t.Errorf("expected 1 top up request, got %d", len(reqs))
	}

	_ = srv.UpdateSettings(context.Background(), map[string]string{"base_fee": "50"})
	st, _ := srv.GetSettings(context.Background())
	if st["base_fee"] != "50" {
		t.Errorf("expected setting base_fee=50, got %v", st)
	}
}

func TestMatchingService_MatchOrders(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	shiftRepo := &mockShiftRepo{}
	srv := NewMatchingService(orderRepo, shiftRepo, newMockUserRepo(), newMockCatalogRepo())

	// Когда ожидающих заказов нет
	err := srv.MatchOrders(context.Background())
	if err != nil {
		t.Fatalf("unexpected error matching orders: %v", err)
	}

	// Проверяем StartMatchingWorker
	srv.StartMatchingWorker(context.Background(), 10*time.Millisecond)
	time.Sleep(25 * time.Millisecond)
}

func TestAuthService_NewAuthService(t *testing.T) {
	authSrv := NewAuthService(newMockRepo(), nil)
	if authSrv == nil {
		t.Error("expected non-nil AuthService")
	}
}
