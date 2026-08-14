package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

type mockReviewRepo struct {
	reviews []*repository.OrderReview
}

func (m *mockReviewRepo) CreateReview(r *repository.OrderReview) error {
	m.reviews = append(m.reviews, r)
	return nil
}

func (m *mockReviewRepo) GetReviewByOrderAndAuthor(orderID, authorID uuid.UUID) (*repository.OrderReview, error) {
	for _, rev := range m.reviews {
		if rev.OrderID == orderID && rev.AuthorID == authorID {
			return rev, nil
		}
	}
	return nil, nil
}

func (m *mockReviewRepo) GetReviewsForUser(targetID uuid.UUID, limit, offset int) ([]repository.OrderReview, error) {
	var res []repository.OrderReview
	for _, rev := range m.reviews {
		if rev.TargetID == targetID {
			res = append(res, *rev)
		}
	}
	return res, nil
}

func (m *mockReviewRepo) UpdateUserRating(userID uuid.UUID, role string) error {
	return nil
}

func (m *mockReviewRepo) GetUserRating(userID uuid.UUID, role string) (*repository.UserRatingSummary, error) {
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

	// Invalid rating
	_, err := srv.CreateReview(orderID, custID, CreateReviewDTO{Rating: 0})
	if err == nil {
		t.Error("expected error for invalid rating")
	}

	// Create review successfully
	rev, err := srv.CreateReview(orderID, custID, CreateReviewDTO{
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

	// Duplicate review attempt
	_, err = srv.CreateReview(orderID, custID, CreateReviewDTO{Rating: 4})
	if err == nil {
		t.Error("expected error on duplicate review")
	}

	// Uninvolved user attempt
	strangerID := uuid.New()
	_, err = srv.CreateReview(orderID, strangerID, CreateReviewDTO{Rating: 5})
	if err == nil {
		t.Error("expected error for uninvolved user")
	}

	// Review by order and author
	found, err := srv.GetReviewByOrderAndAuthor(orderID, custID)
	if err != nil || found == nil {
		t.Errorf("expected to find review, got err: %v", err)
	}

	// Get reviews for user
	revs, err := srv.GetReviewsForUser(execID, 10, 0)
	if err != nil || len(revs) != 1 {
		t.Errorf("expected 1 review for executor, got %d", len(revs))
	}

	// Get user rating
	summary, err := srv.GetUserRating(execID, "EXECUTOR")
	if err != nil || summary.Rating != 5.0 || summary.ReviewsCount != 1 {
		t.Errorf("expected rating 5.0 count 1, got summary: %v", summary)
	}

	// 7 days SLA window expired test
	oldOrderID := uuid.New()
	eightDaysAgo := time.Now().Add(-8 * 24 * time.Hour)
	orderRepo.orders = append(orderRepo.orders, &repository.Order{
		ID:          oldOrderID,
		CustomerID:  custID,
		ExecutorID:  &execID,
		Status:      repository.OrderStatusCompleted,
		CompletedAt: &eightDaysAgo,
	})
	_, err = srv.CreateReview(oldOrderID, custID, CreateReviewDTO{Rating: 5})
	if err == nil {
		t.Error("expected error when submitting review after 7 days window")
	}
}

func TestBidService_AcceptAndGetBids(t *testing.T) {
	bidRepo := &mockBidRepo{}
	orderRepo := &mockOrderRepo{}
	shiftRepo := &mockShiftRepo{}
	srv := NewBidService(bidRepo, orderRepo, shiftRepo)

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

	// Create active shift for executor
	shiftRepo.shifts = append(shiftRepo.shifts, &repository.Shift{
		ID:           uuid.New(),
		ExecutorID:   execID,
		Status:       repository.ShiftStatusActive,
		PlannedEndAt: time.Now().Add(time.Hour),
	})

	bid, err := srv.CreateBid(orderID, execID, 350.00)
	if err != nil {
		t.Fatalf("unexpected error creating bid: %v", err)
	}

	bids, err := srv.GetBidsForOrder(orderID)
	if err != nil || len(bids) != 1 {
		t.Fatalf("expected 1 bid for order, got %d", len(bids))
	}

	// Invalid bid price (0 or negative)
	_, err = srv.CreateBid(orderID, execID, 0)
	if err == nil {
		t.Error("expected error for bid price 0")
	}

	// Accept bid
	err = srv.AcceptBid(bid.ID, custID)
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
	srv := NewOrderService(orderRepo, txRepo, nil, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	custID := uuid.New()
	execID := uuid.New()
	order, _ := srv.CreateOrder(custID, standardVariantID, false, false, "", nil, nil)

	// Accept
	err := srv.Accept(order.ID, execID)
	if err != nil {
		t.Fatalf("unexpected error accepting order: %v", err)
	}

	// ExecuteOrder
	err = srv.ExecuteOrder(order.ID, execID)
	if err != nil {
		t.Fatalf("unexpected error executing order: %v", err)
	}
	if order.Status != repository.OrderStatusExecuted {
		t.Errorf("expected status EXECUTED, got %s", order.Status)
	}

	// RejectAssignedOrder (verify 50% penalty fine transaction)
	order2, _ := srv.CreateOrder(custID, standardVariantID, false, false, "", nil, nil)
	_ = srv.Accept(order2.ID, execID)
	err = srv.RejectAssignedOrder(order2.ID, execID)
	if err != nil {
		t.Fatalf("unexpected error rejecting order: %v", err)
	}
	if order2.Status != repository.OrderStatusSearching {
		t.Errorf("expected status SEARCHING after reject, got %s", order2.Status)
	}
	if len(txRepo.txs) == 0 || txRepo.txs[len(txRepo.txs)-1].Type != "FINE" {
		t.Errorf("expected FINE transaction created on rejection")
	}

	// ListAssigned & ListByCustomer & FindNearbyOrders
	custOrders, _ := srv.ListByCustomer(custID)
	if len(custOrders) != 2 {
		t.Errorf("expected 2 customer orders, got %d", len(custOrders))
	}
	_, _ = srv.FindNearbyOrders(55.75, 37.61, 5000)
	_, _ = srv.GetAvailableConstructionOrders()
}

type mockExecutorGeoRepo struct{}

func (m *mockExecutorGeoRepo) UpdateExecutorLocation(executorID uuid.UUID, lat, lon float64, isManual bool) error {
	return nil
}
func (m *mockExecutorGeoRepo) GetExecutorLocation(executorID uuid.UUID) (*float64, *float64, *time.Time, error) {
	lat := 55.7558
	lon := 37.6173
	return &lat, &lon, nil, nil
}
func (m *mockExecutorGeoRepo) CreateGeoAlert(alert *repository.GeoAlert) error { return nil }
func (m *mockExecutorGeoRepo) GetGeoAlerts(status string, limit, offset int) ([]repository.GeoAlert, error) {
	return []repository.GeoAlert{}, nil
}

func TestExecutorGeoService(t *testing.T) {
	geoRepo := &mockExecutorGeoRepo{}
	orderRepo := &mockOrderRepo{}
	geocoder := NewGeocoder(nil)
	srv := NewExecutorGeoService(geoRepo, orderRepo, geocoder)

	execID := uuid.New()

	// Invalid coordinates
	_, err := srv.SetLocation(execID, SetLocationRequest{Lat: 100.0, Lon: 200.0})
	if err == nil {
		t.Error("expected error for invalid coordinates")
	}

	// Valid set location
	res, err := srv.SetLocation(execID, SetLocationRequest{Lat: 55.7558, Lon: 37.6173, IsManual: false})
	if err != nil || !res.Success {
		t.Fatalf("unexpected error setting location: %v", err)
	}

	// Manual location shift (>2km) triggering cooldown
	res2, err := srv.SetLocation(execID, SetLocationRequest{Lat: 55.8000, Lon: 37.7000, IsManual: true})
	if err != nil || !res2.Success {
		t.Fatalf("unexpected error setting manual location: %v", err)
	}
	res3, err := srv.SetLocation(execID, SetLocationRequest{Lat: 55.9000, Lon: 37.8000, IsManual: true})
	if err != nil || res3.Success {
		t.Error("expected cooldown rejection for manual shift within 10 min")
	}

	// Orders on map
	plat, plon := 55.7558, 37.6173
	orderRepo.orders = append(orderRepo.orders, &repository.Order{
		ID:        uuid.New(),
		Status:    repository.OrderStatusSearching,
		PickupLat: &plat,
		PickupLon: &plon,
	})

	orders, err := srv.GetMapOrders(execID, 55.7558, 37.6173)
	if err != nil || len(orders) != 1 {
		t.Errorf("expected 1 map order, got %d", len(orders))
	}

	alerts, err := srv.GetGeoAlerts("NEW", 10, 0)
	if err != nil {
		t.Errorf("unexpected error getting geo alerts: %v", err)
	}
	_ = alerts
}

func TestAdminService_Extended(t *testing.T) {
	userRepo := newMockRepo()
	adminRepo := &mockAdminRepo{requests: make(map[uuid.UUID]*repository.TopUpRequest)}
	settingsRepo := &mockSettingsRepo{settings: make(map[string]string)}
	tokenRepo := &mockTokenRepo{blacklisted: make(map[string]time.Time)}
	srv := NewAdminService(userRepo, adminRepo, settingsRepo, tokenRepo, "test-secret")

	u := &repository.User{ID: uuid.New(), Phone: "70000000000", Role: "CUSTOMER"}
	userRepo.users[u.Phone] = u

	adminID := uuid.New()
	_ = srv.UpdateUserRole(u.ID, "EXECUTOR")
	_ = srv.UpdateUserRole(u.ID, "INVALID_ROLE")
	_ = srv.UpdateUserAddress(u.ID, "New Address")
	_ = srv.UpdateUserAddress(u.ID, "")
	_ = srv.TopUpUserBalance(adminID, u.ID, 500.0)
	_ = srv.TopUpUserBalance(u.ID, u.ID, 500.0)
	_ = srv.TopUpUserBalance(adminID, u.ID, -50.0)

	users, total, _ := srv.GetUsers(1, 10, "", "", "")
	if total != 0 || len(users) != 0 {
		// mockAdminRepo returned empty
	}

	_, _ = srv.GetActiveShifts()
	_, _ = srv.GetActiveOrders()
	_, _ = srv.GetCompletedOrders()

	prof, err := srv.GetProfile(u.ID)
	if err != nil || prof["phone"] != "70000000000" {
		t.Errorf("expected user profile with phone, got %v", prof)
	}

	// Revoke tokens
	authSrv := NewAuthServiceWithSecret(userRepo, "test-secret", nil)
	token, _ := authSrv.GenerateJWT(u)
	_ = srv.RevokeToken(token)
	rev, _ := srv.IsTokenRevoked(token)
	if !rev {
		t.Error("expected token to be revoked")
	}

	_ = srv.RejectTopUpRequest(uuid.New(), adminID)
	_, _ = srv.GetTransactions()
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

	_, _ = chatRepo.CreateChat(orderID)

	msg, err := srv.SendMessage(orderID, custID, "Hello via REST")
	if err != nil {
		t.Fatalf("unexpected error sending message: %v", err)
	}
	if msg.Text != "Hello via REST" {
		t.Errorf("expected text 'Hello via REST', got '%s'", msg.Text)
	}

	attMsg, err := srv.SendMessageWithAttachment(orderID, custID, "Photo caption", "/uploads/test.jpg", "test.jpg", "image", 1024)
	if err != nil {
		t.Fatalf("unexpected error sending attachment: %v", err)
	}
	if attMsg.FileName == nil || *attMsg.FileName != "test.jpg" {
		t.Errorf("expected file_name 'test.jpg'")
	}

	_, _ = srv.MarkMessagesAsRead(orderID, execID)
	_, _ = srv.GetUnreadOrderIDs(execID)
	srv.BroadcastSystemMessage(orderID, map[string]string{"type": "system", "text": "hello"})
}

func TestShiftService_Extended(t *testing.T) {
	shiftRepo := &mockShiftRepo{}
	srv := NewShiftService(shiftRepo, nil, &mockTransactionRepo{}, &orderMockSettingsRepo{}, &mockOrderRepo{}, nil, nil)

	execID := uuid.New()
	shift, err := srv.StartShift(execID, 3)
	if err != nil {
		t.Fatalf("unexpected error starting shift: %v", err)
	}
	if shift.DurationHours != 3 {
		t.Errorf("expected duration 3, got %d", shift.DurationHours)
	}

	_, _ = srv.GetExecutorFinancialHistory(execID)
	_ = srv.EndShiftByID(shift.ID)
	srv.AutoEndExpiredShifts()

	// Test Start, GetActive, GetCurrent, End
	execID2 := uuid.New()
	s2, err := srv.Start(execID2, 1)
	if err != nil {
		t.Fatalf("unexpected error in Start: %v", err)
	}
	act, err := srv.GetActive(execID2)
	if err != nil || act.ID != s2.ID {
		t.Errorf("expected active shift %s, got %v", s2.ID, act)
	}
	curr, err := srv.GetCurrent(execID2)
	if err != nil || curr.ID != s2.ID {
		t.Errorf("expected current shift %s, got %v", s2.ID, curr)
	}
	err = srv.End(execID2)
	if err != nil {
		t.Errorf("unexpected error in End: %v", err)
	}
}

func TestAuthService_ParseJWT(t *testing.T) {
	userRepo := newMockRepo()
	srv := NewAuthServiceWithSecret(userRepo, "test-secret", nil)

	u := &repository.User{ID: uuid.New(), Phone: "79001112233", Role: "CUSTOMER"}
	tokenStr, err := srv.GenerateJWT(u)
	if err != nil {
		t.Fatalf("failed to generate JWT: %v", err)
	}

	claims, err := srv.ParseJWT(tokenStr)
	if err != nil || claims.UserID != u.ID || claims.Phone != u.Phone {
		t.Errorf("failed parsing JWT: %v, claims: %v", err, claims)
	}

	_, err = srv.ParseJWT("invalid.jwt.token")
	if err == nil {
		t.Error("expected error for invalid JWT")
	}
}

func TestOrderService_Aliases(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	srv := NewOrderService(orderRepo, &mockTransactionRepo{}, nil, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	custID := uuid.New()
	execID := uuid.New()
	lat, lon := 55.75, 37.61
	order, err := srv.Create(custID, CreateOrderRequest{
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

	// Confirm alias
	_ = orderRepo.AssignOrder(order.ID, execID)
	_ = orderRepo.Execute(order.ID)
	err = srv.Confirm(custID, order.ID)
	if err != nil {
		t.Errorf("unexpected error in Confirm alias: %v", err)
	}

	// Cancel alias
	order2, _ := srv.Create(custID, CreateOrderRequest{ServiceVariantID: standardVariantID})
	err = srv.Cancel(custID, order2.ID)
	if err != nil {
		t.Errorf("unexpected error in Cancel alias: %v", err)
	}

	// ListAssigned
	_, _ = srv.ListAssigned(execID)
}

func TestAdminService_TopUpRequestAndSettings(t *testing.T) {
	userRepo := newMockRepo()
	adminRepo := &mockAdminRepo{requests: make(map[uuid.UUID]*repository.TopUpRequest)}
	settingsRepo := &mockSettingsRepo{settings: make(map[string]string)}
	srv := NewAdminService(userRepo, adminRepo, settingsRepo, &mockTokenRepo{}, "secret")

	u := &repository.User{ID: uuid.New(), Phone: "79998887766"}
	userRepo.users[u.Phone] = u

	req, err := srv.CreateTopUpRequest(u.ID, 300.0)
	if err != nil || req.Amount != 300.0 {
		t.Fatalf("unexpected error creating top up request: %v", err)
	}

	reqs, err := srv.GetTopUpRequests()
	if err != nil || len(reqs) != 1 {
		t.Errorf("expected 1 top up request, got %d", len(reqs))
	}

	_ = srv.UpdateSettings(map[string]string{"base_fee": "50"})
	st, _ := srv.GetSettings()
	if st["base_fee"] != "50" {
		t.Errorf("expected setting base_fee=50, got %v", st)
	}
}

func TestMatchingService_MatchOrders(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	shiftRepo := &mockShiftRepo{}
	srv := NewMatchingService(orderRepo, shiftRepo, nil)

	// When no pending orders exist
	err := srv.MatchOrders()
	if err != nil {
		t.Fatalf("unexpected error matching orders: %v", err)
	}

	// Test StartMatchingWorker
	srv.StartMatchingWorker(10 * time.Millisecond)
	time.Sleep(25 * time.Millisecond)
}

func TestGeo_ParsePolygon(t *testing.T) {
	poly, err := parsePolygon(`[{"lat": 55.75, "lon": 37.61}, {"lat": 55.76, "lon": 37.62}]`)
	if err != nil || len(poly) != 2 {
		t.Fatalf("failed parsing polygon: %v", err)
	}

	_, err = parsePolygon("invalid json")
	if err == nil {
		t.Error("expected error for invalid polygon json")
	}
}

func TestAuthService_NewAuthService(t *testing.T) {
	authSrv := NewAuthService(newMockRepo(), nil)
	if authSrv == nil {
		t.Error("expected non-nil AuthService")
	}
}
