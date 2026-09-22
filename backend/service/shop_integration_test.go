package service

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Магазин на настоящем Postgres: покупка — это блокировка товара, списание,
// выдача и событие в одной транзакции, и проверить, что отказ откатывает всё,
// можно только там.
//
//	ORDER_TEST_DSN="postgres://postgres:x@localhost:55432/healthlogin?sslmode=disable" \
//	    go test ./service/ -run Shop

type shopFixture struct {
	t      *testing.T
	db     *sql.DB
	srv    *ShopService
	gifts  repository.GiftRepository
	shop   repository.ShopRepository
	perks  repository.PerkRepository
	orders repository.ShopOrderRepository
}

func newShopFixture(t *testing.T, overrides map[string]string) *shopFixture {
	t.Helper()
	db := openTestDB(t)
	values := map[string]string{
		SettingShopEnabled: "1", SettingShopOfferVersion: "1",
		SettingOrderCommissionPercent: "10", SettingAchievementLevelPoints: "500",
		SettingAchievementLevelDiscountPP: "1",
	}
	for k, v := range overrides {
		values[k] = v
	}
	// Настройки подменяются поверх настоящих, а не пишутся в таблицу: пакеты
	// тестов идут параллельно на одной базе.
	settings := settingsOverride{repository.NewSettingsRepository(db), values}
	f := &shopFixture{t: t, db: db,
		gifts: repository.NewGiftRepository(db), shop: repository.NewShopRepository(db),
		perks: repository.NewPerkRepository(db), orders: repository.NewShopOrderRepository(db)}
	// Покупки и возвраты двигают общий счёт SHOP на общей базе. Его баланс
	// возвращается как был: другие тесты считают от него вывод выручки.
	var shopBefore money.Amount
	if err := db.QueryRow(`SELECT balance FROM system_accounts WHERE code = $1`, repository.AccountShop).Scan(&shopBefore); err != nil {
		t.Fatalf("shop balance: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`UPDATE system_accounts SET balance = $2 WHERE code = $1`, repository.AccountShop, shopBefore)
	})
	ledger := NewLedger(repository.NewTransactionRepository(db), repository.NewSystemAccountRepository(db))
	levels := NewLevels(repository.NewAchievementRepository(db), settings).WithPerks(f.perks, nil)
	f.srv = NewShopService(f.shop, f.orders, f.perks, f.gifts, ledger, levels, settings).
		WithEvents(repository.NewEventRepository(db)).
		WithMail(repository.NewMailRepository(db))
	return f
}

// user заводит покупателя с балансом и убирает за ним всё, что магазин успел
// на него записать.
func (f *shopFixture) user(role string, balance money.Amount) *repository.User {
	f.t.Helper()
	id := uuid.New()
	if _, err := f.db.Exec(
		`INSERT INTO users (id, role, phone, password, balance, status) VALUES ($1, $2, $3, 'x', $4, 'ACTIVE')`,
		id, role, "+7996"+id.String()[:7], balance); err != nil {
		f.t.Fatalf("seed user: %v", err)
	}
	f.t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM transactions WHERE user_id = $1`,
			`DELETE FROM user_gifts WHERE user_id = $1`,
			`DELETE FROM user_perks WHERE user_id = $1`,
			`DELETE FROM user_mail WHERE user_id = $1`,
			`DELETE FROM domain_events WHERE subject_id = $1`,
			`DELETE FROM support_messages WHERE chat_id IN (SELECT id FROM support_chats WHERE user_id = $1)`,
			`DELETE FROM support_chats WHERE user_id = $1`,
			`DELETE FROM shop_orders WHERE user_id = $1`,
			`DELETE FROM users WHERE id = $1`,
		} {
			_, _ = f.db.Exec(q, id)
		}
	})
	return &repository.User{ID: id, Role: role, Status: "ACTIVE", Balance: balance}
}

func (f *shopFixture) balance(id uuid.UUID) money.Amount {
	f.t.Helper()
	var b money.Amount
	if err := f.db.QueryRow(`SELECT balance FROM users WHERE id = $1`, id).Scan(&b); err != nil {
		f.t.Fatalf("balance: %v", err)
	}
	return b
}

func (f *shopFixture) product(p *repository.ShopProduct) *repository.ShopProduct {
	f.t.Helper()
	p.IsActive = true
	if p.Title == nil {
		p.Title = map[string]interface{}{"ru": "Товар"}
	}
	if p.Category == "" {
		p.Category = "test"
	}
	if err := f.shop.UpsertProduct(context.Background(), p); err != nil {
		f.t.Fatalf("seed product: %v", err)
	}
	f.t.Cleanup(func() {
		_, _ = f.db.Exec(`DELETE FROM shop_products WHERE id = $1`, p.ID)
	})
	return p
}

func (f *shopFixture) perkProduct(kind string, value *float64, price money.Amount, limit *int) *repository.ShopProduct {
	days := 30
	return f.product(&repository.ShopProduct{
		Kind: repository.ShopKindPerk, PerkKind: &kind, PerkValue: value, PerkDays: &days,
		Price: price, Roles: []string{"EXECUTOR"}, MaxActivePerUser: limit,
	})
}

func (f *shopFixture) physical(stock int) (*repository.ShopProduct, string) {
	f.t.Helper()
	code := "shop-it-" + uuid.New().String()[:8]
	gift := &repository.Gift{Code: code, Kind: repository.GiftKindPhysical,
		Title: map[string]interface{}{"ru": "Футболка"}, Stock: &stock, IsActive: true}
	if err := f.gifts.Upsert(context.Background(), gift); err != nil {
		f.t.Fatalf("seed gift: %v", err)
	}
	// Удаление подарка — после покупок и товара: Cleanup идёт в обратном порядке.
	f.t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM gifts WHERE code = $1`, code) })
	p := f.product(&repository.ShopProduct{
		Kind: repository.ShopKindPhysical, GiftCode: &code, Price: money.FromRubles(1500), MaxQtyPerOrder: 5,
		Variants:           []repository.ShopProductVariant{{Code: "M"}, {Code: "L"}},
		FulfillmentMethods: []string{repository.ShopFulfillmentDelivery},
	})
	return p, code
}

func (f *shopFixture) stock(code string) int {
	f.t.Helper()
	var stock int
	if err := f.db.QueryRow(`SELECT stock FROM gifts WHERE code = $1`, code).Scan(&stock); err != nil {
		f.t.Fatalf("stock: %v", err)
	}
	return stock
}

func buy(p *repository.ShopProduct) PurchaseRequest {
	return PurchaseRequest{ProductID: p.ID, RequestID: uuid.New(), ExpectedPrice: p.Price, OfferVersion: 1, Quantity: 1}
}

func buyShirt(p *repository.ShopProduct) PurchaseRequest {
	req := buy(p)
	req.Variant = "M"
	req.Fulfillment.Method = repository.ShopFulfillmentDelivery
	req.Fulfillment.Address, req.Fulfillment.Recipient, req.Fulfillment.Phone = "Москва, Тверская, 1", "Иван", "+79990000000"
	return req
}

func shopCode(err error) string {
	var shopErr *ShopError
	if errors.As(err, &shopErr) {
		return shopErr.Code
	}
	return ""
}

func TestShopPurchasePerkQueuesAndIsIdempotentIntegration(t *testing.T) {
	f := newShopFixture(t, nil)
	ctx := context.Background()
	buyer := f.user("EXECUTOR", money.FromRubles(5000))
	price := money.FromRubles(1000)
	limit := 2
	product := f.perkProduct(PerkKindCommissionMultiplier, floatPtr(0.5), price, &limit)

	req := buy(product)
	first, err := f.srv.Purchase(ctx, buyer, req)
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	if first.Status != repository.ShopOrderCompleted || len(first.Perks) != 1 {
		t.Fatalf("order = %s with %d perks, want COMPLETED with one", first.Status, len(first.Perks))
	}
	if got := f.balance(buyer.ID); got != money.FromRubles(4000) {
		t.Fatalf("balance = %s, want 4000", got)
	}

	// Повтор того же запроса — та же покупка, второго списания нет.
	again, err := f.srv.Purchase(ctx, buyer, req)
	if err != nil || again.ID != first.ID {
		t.Fatalf("repeat returned %v, %v; want the same order %s", again, err, first.ID)
	}
	if got := f.balance(buyer.ID); got != money.FromRubles(4000) {
		t.Fatalf("repeat charged again: balance %s", got)
	}

	// Вторая покупка встаёт в очередь за первой, а не поверх неё.
	second, err := f.srv.Purchase(ctx, buyer, buy(product))
	if err != nil {
		t.Fatalf("second purchase: %v", err)
	}
	if !second.Perks[0].StartsAt.Equal(first.Perks[0].ExpiresAt) {
		t.Errorf("second perk starts %s, want the end of the first %s", second.Perks[0].StartsAt, first.Perks[0].ExpiresAt)
	}

	// Лимит очереди — два: третья отклоняется, деньги не списаны.
	if _, err := f.srv.Purchase(ctx, buyer, buy(product)); shopCode(err) != ShopErrLimitReached {
		t.Fatalf("third purchase: %v, want limit_reached", err)
	}
	if got := f.balance(buyer.ID); got != money.FromRubles(3000) {
		t.Errorf("balance = %s after a refused purchase, want 3000", got)
	}

	// Оплата записана против покупки и счёта SHOP.
	var charged money.Amount
	if err := f.db.QueryRow(`SELECT COALESCE(SUM(amount), 0) FROM transactions
		WHERE shop_order_id = $1 AND type = 'SHOP_PURCHASE' AND counterparty = 'SHOP'`, first.ID).Scan(&charged); err != nil {
		t.Fatalf("read charge: %v", err)
	}
	if charged != price {
		t.Errorf("charged %s against the order, want %s", charged, price)
	}
}

// Выданная вручную привилегия встаёт в ту же очередь: беспроцентный день
// поверх купленного множителя не съедает его дни.
func TestShopGrantedPerkQueuesBehindThePurchasedOneIntegration(t *testing.T) {
	f := newShopFixture(t, nil)
	ctx := context.Background()
	buyer := f.user("EXECUTOR", money.FromRubles(5000))
	admin := f.user("ADMIN", 0)
	product := f.perkProduct(PerkKindCommissionMultiplier, floatPtr(0.5), money.FromRubles(1000), nil)

	bought, err := f.srv.Purchase(ctx, buyer, buy(product))
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	granted, err := f.srv.GrantPerk(ctx, admin.ID, buyer.ID, GrantPerkRequest{Kind: PerkKindCommissionFree, Days: 1, Reason: "компенсация"})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if !granted.StartsAt.Equal(bought.Perks[0].ExpiresAt) {
		t.Errorf("granted perk starts %s, want after the purchased one %s", granted.StartsAt, bought.Perks[0].ExpiresAt)
	}
	// Сейчас действует купленный множитель, а не бесплатный день.
	level := f.srv.levels.For(ctx, nil, buyer.ID)
	if level.PerkKind != PerkKindCommissionMultiplier || level.Percent != 5 {
		t.Errorf("active perk %s at %v%%, want the multiplier at 5%%", level.PerkKind, level.Percent)
	}

	if _, err := f.srv.GrantPerk(ctx, admin.ID, buyer.ID, GrantPerkRequest{Kind: PerkKindCommissionMultiplier, Value: floatPtr(1.5), Days: 3, Reason: "x"}); shopCode(err) != ShopErrValidation {
		t.Errorf("multiplier 1.5 granted: %v", err)
	}
	if _, err := f.srv.RevokePerk(ctx, admin.ID, granted.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := f.srv.RevokePerk(ctx, admin.ID, granted.ID); shopCode(err) != ShopErrNotFound {
		t.Errorf("second revoke: %v, want not_found", err)
	}
}

// Продавать половину от нуля нельзя: ставка по уровню 0 — отказ.
func TestShopRefusesAUselessPerkIntegration(t *testing.T) {
	f := newShopFixture(t, map[string]string{SettingOrderCommissionPercent: "0"})
	buyer := f.user("EXECUTOR", money.FromRubles(5000))
	product := f.perkProduct(PerkKindCommissionMultiplier, floatPtr(0.5), money.FromRubles(1000), nil)

	card, err := f.srv.Product(context.Background(), buyer, product.ID)
	if err != nil || card.PerkQuote == nil || !card.PerkQuote.Useless {
		t.Fatalf("card = %+v, %v; want a useless quote", card, err)
	}
	if _, err := f.srv.Purchase(context.Background(), buyer, buy(product)); shopCode(err) != ShopErrPerkUseless {
		t.Fatalf("purchase: %v, want perk_useless", err)
	}
	if got := f.balance(buyer.ID); got != money.FromRubles(5000) {
		t.Errorf("balance = %s after a refused purchase", got)
	}
}

// Каждый отказ оставляет и баланс, и склад как были.
func TestShopRefusalsChargeNothingIntegration(t *testing.T) {
	f := newShopFixture(t, nil)
	ctx := context.Background()
	shirt, code := f.physical(3)
	perkForExecutors := f.perkProduct(PerkKindCommissionMultiplier, floatPtr(0.5), money.FromRubles(1000), nil)
	buyer := f.user("CUSTOMER", money.FromRubles(2000))
	poor := f.user("CUSTOMER", money.FromRubles(100))

	priceChanged := buyShirt(shirt)
	priceChanged.ExpectedPrice = money.FromRubles(1000)
	offerChanged := buyShirt(shirt)
	offerChanged.OfferVersion = 0
	noAddress := buyShirt(shirt)
	noAddress.Fulfillment.Address = ""
	tooMany := buyShirt(shirt)
	tooMany.Quantity = 6

	for _, tc := range []struct {
		name   string
		user   *repository.User
		req    PurchaseRequest
		code   string
		status int
	}{
		{"price changed", buyer, priceChanged, ShopErrPriceChanged, http.StatusConflict},
		{"offer changed", buyer, offerChanged, ShopErrOfferChanged, http.StatusConflict},
		{"no address", buyer, noAddress, ShopErrValidation, http.StatusUnprocessableEntity},
		{"too many", buyer, tooMany, ShopErrInvalidRequest, http.StatusBadRequest},
		{"insufficient funds", poor, buyShirt(shirt), ShopErrInsufficientFunds, http.StatusUnprocessableEntity},
		// Товар чужой роли не раскрывается: 404, а не 403.
		{"hidden from the role", buyer, buy(perkForExecutors), ShopErrNotFound, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := f.balance(tc.user.ID)
			_, err := f.srv.Purchase(ctx, tc.user, tc.req)
			var shopErr *ShopError
			if !errors.As(err, &shopErr) || shopErr.Code != tc.code || shopErr.Status != tc.status {
				t.Fatalf("got %v, want %s (%d)", err, tc.code, tc.status)
			}
			if got := f.balance(tc.user.ID); got != before {
				t.Errorf("balance moved from %s to %s", before, got)
			}
			if got := f.stock(code); got != 3 {
				t.Errorf("stock = %d, want 3 untouched", got)
			}
		})
	}
}

// Последняя единица и две одновременные покупки: одна проходит, вторая
// получает out_of_stock и не платит.
func TestShopLastUnitGoesToOneBuyerIntegration(t *testing.T) {
	f := newShopFixture(t, nil)
	ctx := context.Background()
	shirt, code := f.physical(1)
	a := f.user("CUSTOMER", money.FromRubles(2000))
	b := f.user("CUSTOMER", money.FromRubles(2000))

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, buyer := range []*repository.User{a, b} {
		wg.Add(1)
		go func(i int, buyer *repository.User) {
			defer wg.Done()
			_, errs[i] = f.srv.Purchase(ctx, buyer, buyShirt(shirt))
		}(i, buyer)
	}
	wg.Wait()

	won := 0
	for i, err := range errs {
		switch {
		case err == nil:
			won++
		case shopCode(err) == ShopErrOutOfStock:
			if got := f.balance([]*repository.User{a, b}[i].ID); got != money.FromRubles(2000) {
				t.Errorf("the losing buyer was charged: balance %s", got)
			}
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d purchases succeeded, want exactly one", won)
	}
	if got := f.stock(code); got != 0 {
		t.Errorf("stock = %d, want 0", got)
	}
}

// Вещь проходит PAID → PROCESSING → SHIPPED с треком, а погашение купона
// закрывает покупку. Лишний переход — 409.
func TestShopPhysicalOrderLifecycleIntegration(t *testing.T) {
	f := newShopFixture(t, nil)
	ctx := context.Background()
	shirt, _ := f.physical(5)
	buyer := f.user("CUSTOMER", money.FromRubles(5000))
	admin := f.user("ADMIN", 0)

	req := buyShirt(shirt)
	req.Quantity = 2
	order, err := f.srv.Purchase(ctx, buyer, req)
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	if order.Status != repository.ShopOrderPaid || len(order.Coupons) != 2 || order.Total != money.FromRubles(3000) {
		t.Fatalf("order = %s, %d coupons, total %s", order.Status, len(order.Coupons), order.Total)
	}

	if _, err := f.srv.SetStatus(ctx, admin.ID, order.ID, repository.ShopOrderShipped, ""); shopCode(err) != ShopErrInvalidTransition {
		t.Fatalf("PAID → SHIPPED: %v, want invalid_transition", err)
	}
	if _, err := f.srv.SetStatus(ctx, admin.ID, order.ID, repository.ShopOrderProcessing, ""); err != nil {
		t.Fatalf("→ PROCESSING: %v", err)
	}
	if _, err := f.srv.SetStatus(ctx, admin.ID, order.ID, repository.ShopOrderShipped, ""); shopCode(err) != ShopErrValidation {
		t.Fatalf("delivery shipped without a track: %v", err)
	}
	shipped, err := f.srv.SetStatus(ctx, admin.ID, order.ID, repository.ShopOrderShipped, "RA123456789RU")
	if err != nil || shipped.Fulfillment["track"] != "RA123456789RU" {
		t.Fatalf("→ SHIPPED: %v, fulfillment %v", err, shipped)
	}

	// Погашение первого купона покупку не закрывает, последнего — закрывает.
	for i, c := range order.Coupons {
		redeemed, err := f.gifts.RedeemCoupon(ctx, c.CouponCode, admin.ID)
		if err != nil {
			t.Fatalf("redeem: %v", err)
		}
		if err := f.srv.OnCouponRedeemed(ctx, redeemed); err != nil {
			t.Fatalf("complete: %v", err)
		}
		got, _ := f.srv.AdminOrder(ctx, order.ID)
		want := repository.ShopOrderShipped
		if i == len(order.Coupons)-1 {
			want = repository.ShopOrderCompleted
		}
		if got.Status != want {
			t.Errorf("after %d redeemed coupons status = %s, want %s", i+1, got.Status, want)
		}
	}
}

// Отмена вещи: полный возврат, купоны аннулированы, склад по галочке; повтор —
// 409. Возврат записан против покупки.
func TestShopCancelPhysicalRefundsAndRestocksIntegration(t *testing.T) {
	f := newShopFixture(t, nil)
	ctx := context.Background()
	shirt, code := f.physical(5)
	buyer := f.user("CUSTOMER", money.FromRubles(5000))
	admin := f.user("ADMIN", 0)

	order, err := f.srv.Purchase(ctx, buyer, buyShirt(shirt))
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	if _, err := f.srv.Cancel(ctx, admin.ID, order.ID, CancelRequest{Restock: true}); shopCode(err) != ShopErrValidation {
		t.Fatalf("cancel without a reason: %v", err)
	}
	canceled, err := f.srv.Cancel(ctx, admin.ID, order.ID, CancelRequest{Reason: "обращение в поддержку", Restock: true})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if canceled.Status != repository.ShopOrderCanceled || canceled.RefundedAmount != shirt.Price {
		t.Errorf("order = %s refunded %s, want CANCELED refunding %s", canceled.Status, canceled.RefundedAmount, shirt.Price)
	}
	if got := f.balance(buyer.ID); got != money.FromRubles(5000) {
		t.Errorf("balance = %s, want the full 5000 back", got)
	}
	if got := f.stock(code); got != 5 {
		t.Errorf("stock = %d, want 5 after restock", got)
	}
	for _, c := range canceled.Coupons {
		if c.Status != repository.GiftStatusCanceled {
			t.Errorf("coupon %s is %s, want CANCELED", c.CouponCode, c.Status)
		}
	}
	var refunds int
	for _, tx := range canceled.Transactions {
		if tx.Type == string(repository.TransactionTypeShopRefund) {
			refunds++
		}
	}
	if refunds != 1 {
		t.Errorf("%d SHOP_REFUND entries on the order, want 1", refunds)
	}
	if _, err := f.srv.Cancel(ctx, admin.ID, order.ID, CancelRequest{Reason: "ещё раз"}); shopCode(err) != ShopErrAlreadyCanceled {
		t.Errorf("second cancel: %v, want already_canceled", err)
	}
}

// Отказ от привилегии, которая ещё не началась, возвращает всё, и её место в
// очереди освобождается; сумма больше уплаченного не принимается.
func TestShopCancelQueuedPerkRefundsInFullIntegration(t *testing.T) {
	f := newShopFixture(t, nil)
	ctx := context.Background()
	buyer := f.user("EXECUTOR", money.FromRubles(5000))
	admin := f.user("ADMIN", 0)
	product := f.perkProduct(PerkKindCommissionDiscountPP, floatPtr(5), money.FromRubles(1000), nil)

	if _, err := f.srv.Purchase(ctx, buyer, buy(product)); err != nil {
		t.Fatalf("first purchase: %v", err)
	}
	queued, err := f.srv.Purchase(ctx, buyer, buy(product))
	if err != nil {
		t.Fatalf("second purchase: %v", err)
	}
	quote, err := f.srv.RefundQuote(ctx, queued.ID)
	if err != nil || quote.Suggested != money.FromRubles(1000) {
		t.Fatalf("quote = %+v, %v; want the full price for a perk that has not started", quote, err)
	}
	tooMuch := money.FromRubles(1001)
	if _, err := f.srv.Cancel(ctx, admin.ID, queued.ID, CancelRequest{Reason: "x", Amount: &tooMuch}); shopCode(err) != ShopErrValidation {
		t.Fatalf("refund above the price: %v", err)
	}
	canceled, err := f.srv.Cancel(ctx, admin.ID, queued.ID, CancelRequest{Reason: "передумал"})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if canceled.Perks[0].RevokedAt == nil {
		t.Error("the perk of a canceled purchase is still active")
	}
	if got := f.balance(buyer.ID); got != money.FromRubles(4000) {
		t.Errorf("balance = %s, want 4000", got)
	}
}

// Номер покупки в чате поддержки становится ссылкой только для покупок
// владельца чата: чужой номер не размечается.
func TestShopOrderLinksOnlyTheOwnersOrdersIntegration(t *testing.T) {
	f := newShopFixture(t, nil)
	ctx := context.Background()
	shirt, _ := f.physical(5)
	owner := f.user("CUSTOMER", money.FromRubles(5000))
	stranger := f.user("CUSTOMER", money.FromRubles(5000))
	mine, err := f.srv.Purchase(ctx, owner, buyShirt(shirt))
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	theirs, err := f.srv.Purchase(ctx, stranger, buyShirt(shirt))
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	message := &repository.Message{SenderID: owner.ID,
		Text: "Возврат по покупке №" + itoa(mine.Number) + ", и ещё №" + itoa(theirs.Number)}
	if err := f.srv.ShopOrderLinks(ctx, owner.ID, []*repository.Message{message}); err != nil {
		t.Fatalf("links: %v", err)
	}
	if len(message.ShopOrders) != 1 || message.ShopOrders[0].ID != mine.ID {
		t.Errorf("links = %+v, want only the owner's order %s", message.ShopOrders, mine.ID)
	}
}

// Напоминание о конце привилегии — одно письмо, и только когда за ней в
// очереди ничего нет.
func TestShopPerkReminderIsSentOnceIntegration(t *testing.T) {
	f := newShopFixture(t, nil)
	ctx := context.Background()
	alone := f.user("EXECUTOR", 0)
	followed := f.user("EXECUTOR", 0)
	now := time.Now()
	for _, p := range []*repository.UserPerk{
		{UserID: alone.ID, Kind: PerkKindCommissionFree, StartsAt: now.AddDate(0, 0, -5), ExpiresAt: now.Add(48 * time.Hour)},
		{UserID: followed.ID, Kind: PerkKindCommissionFree, StartsAt: now.AddDate(0, 0, -5), ExpiresAt: now.Add(48 * time.Hour)},
		{UserID: followed.ID, Kind: PerkKindCommissionFree, StartsAt: now.Add(48 * time.Hour), ExpiresAt: now.AddDate(0, 0, 10)},
	} {
		if err := f.perks.Create(ctx, nil, p); err != nil {
			t.Fatalf("seed perk: %v", err)
		}
	}
	if _, err := f.srv.SendPerkReminders(ctx); err != nil {
		t.Fatalf("remind: %v", err)
	}
	if _, err := f.srv.SendPerkReminders(ctx); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	count := func(id uuid.UUID) int {
		var n int
		_ = f.db.QueryRow(`SELECT COUNT(*) FROM user_mail WHERE user_id = $1 AND kind = 'SHOP'`, id).Scan(&n)
		return n
	}
	if got := count(alone.ID); got != 1 {
		t.Errorf("%d reminders for a perk that ends alone, want 1", got)
	}
	if got := count(followed.ID); got != 0 {
		t.Errorf("%d reminders for a perk with another behind it, want 0", got)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// Две покупки привилегии одновременно не начинаются в один момент: строка
// покупателя блокируется, и вторая читает конец очереди после первой.
func TestShopConcurrentPerkPurchasesDoNotOverlapIntegration(t *testing.T) {
	f := newShopFixture(t, nil)
	ctx := context.Background()
	buyer := f.user("EXECUTOR", money.FromRubles(5000))
	product := f.perkProduct(PerkKindCommissionFree, nil, money.FromRubles(500), nil)

	var wg sync.WaitGroup
	orders := make([]*repository.ShopOrder, 2)
	errs := make([]error, 2)
	for i := range orders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			orders[i], errs[i] = f.srv.Purchase(ctx, buyer, buy(product))
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("purchase: %v", err)
		}
	}
	a, b := orders[0].Perks[0], orders[1].Perks[0]
	if a.StartsAt.Before(b.ExpiresAt) && b.StartsAt.Before(a.ExpiresAt) {
		t.Errorf("perks overlap: %s–%s and %s–%s", a.StartsAt, a.ExpiresAt, b.StartsAt, b.ExpiresAt)
	}
}

// Выключенный магазин: витрина пуста и говорит клиенту спрятать пункт меню,
// покупка отклоняется.
func TestShopDisabledHidesTheStorefrontIntegration(t *testing.T) {
	f := newShopFixture(t, map[string]string{SettingShopEnabled: "0"})
	ctx := context.Background()
	buyer := f.user("EXECUTOR", money.FromRubles(5000))
	product := f.perkProduct(PerkKindCommissionFree, nil, money.FromRubles(500), nil)

	front, err := f.srv.Storefront(ctx, buyer, "")
	if err != nil || front.Enabled || len(front.Products) != 0 {
		t.Fatalf("storefront = %+v, %v; want disabled and empty", front, err)
	}
	if _, err := f.srv.Purchase(ctx, buyer, buy(product)); shopCode(err) != ShopErrShopDisabled {
		t.Errorf("purchase in a closed shop: %v", err)
	}
}
