package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// seedShopGift заводит подарок, на который ссылается товар родов PHYSICAL и
// CERTIFICATE: склад и пул кодов общие с ачивками.
func seedShopGift(t *testing.T, db *sql.DB, code, kind string, stock *int) {
	t.Helper()
	gift := &repository.Gift{
		Code: code, Kind: kind, Title: map[string]interface{}{"ru": code},
		Stock: stock, IsActive: true,
	}
	if err := repository.NewGiftRepository(db).Upsert(context.Background(), gift); err != nil {
		t.Fatalf("seed gift: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM gift_codes WHERE gift_code = $1`, code)
		_, _ = db.Exec(`DELETE FROM shop_products WHERE gift_code = $1`, code)
		_, _ = db.Exec(`DELETE FROM gifts WHERE code = $1`, code)
	})
}

func shopProductByID(products []*repository.ShopProduct, id uuid.UUID) *repository.ShopProduct {
	for _, p := range products {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func TestShopProductRoundTrip(t *testing.T) {
	db := testDB(t)
	repo := repository.NewShopRepository(db)
	ctx := context.Background()

	stock := 10
	seedShopGift(t, db, "shop-roundtrip-shirt", repository.GiftKindPhysical, &stock)

	multiplier, discount := 0.5, 5.0
	days, limit := 30, 3
	products := []*repository.ShopProduct{
		{
			Kind: repository.ShopKindPerk, Category: "perks",
			Title: map[string]interface{}{"ru": "Комиссия вдвое меньше", "en": "Half commission"},
			Price: money.FromRubles(1000), PerkKind: strPtr("COMMISSION_MULTIPLIER"),
			PerkValue: &multiplier, PerkDays: &days, MaxActivePerUser: &limit,
			Roles: []string{"EXECUTOR"}, IsActive: true, SortOrder: 1,
			Images: []string{"/uploads/shop/perk.png"},
		},
		{
			Kind: repository.ShopKindPhysical, Category: "merch",
			Title: map[string]interface{}{"ru": "Футболка"}, Price: money.FromRubles(1500),
			GiftCode: strPtr("shop-roundtrip-shirt"), MaxQtyPerOrder: 5,
			Variants: []repository.ShopProductVariant{{Code: "M", Title: map[string]interface{}{"ru": "M"}}},
			FulfillmentMethods: []string{"PICKUP", "DELIVERY"}, IsActive: true,
		},
		{
			Kind: repository.ShopKindPerk, Category: "perks",
			Title: map[string]interface{}{"ru": "Минус 5 пунктов"}, Price: money.FromRubles(700),
			PerkKind: strPtr("COMMISSION_DISCOUNT_PP"), PerkValue: &discount, PerkDays: &days,
			IsActive: false,
		},
	}
	for _, p := range products {
		if err := repo.UpsertProduct(ctx, p); err != nil {
			t.Fatalf("upsert %s: %v", p.Kind, err)
		}
	}
	t.Cleanup(func() {
		for _, p := range products {
			_, _ = db.Exec(`DELETE FROM shop_products WHERE id = $1`, p.ID)
		}
	})

	got, err := repo.GetProduct(ctx, products[0].ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Price != products[0].Price {
		t.Errorf("price = %s, expected %s", got.Price, products[0].Price)
	}
	if got.PerkKind == nil || *got.PerkKind != "COMMISSION_MULTIPLIER" {
		t.Errorf("perk kind = %v", got.PerkKind)
	}
	if got.PerkValue == nil || *got.PerkValue != 0.5 {
		t.Errorf("perk value = %v", got.PerkValue)
	}
	if got.PerkDays == nil || *got.PerkDays != 30 {
		t.Errorf("perk days = %v", got.PerkDays)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "EXECUTOR" {
		t.Errorf("roles = %v", got.Roles)
	}
	if len(got.Images) != 1 {
		t.Errorf("images = %v", got.Images)
	}
	if !got.InStock {
		t.Error("a perk is never out of stock")
	}

	shirt, err := repo.GetProduct(ctx, products[1].ID)
	if err != nil {
		t.Fatalf("get shirt: %v", err)
	}
	if len(shirt.Variants) != 1 || shirt.Variants[0].Code != "M" {
		t.Errorf("variants = %+v", shirt.Variants)
	}
	if len(shirt.FulfillmentMethods) != 2 {
		t.Errorf("fulfillment methods = %v", shirt.FulfillmentMethods)
	}
	if !shirt.InStock || shirt.StockCount == nil || *shirt.StockCount != 10 {
		t.Errorf("shirt stock: in_stock=%v count=%v", shirt.InStock, shirt.StockCount)
	}

	if _, err := repo.GetProduct(ctx, uuid.New()); !errors.Is(err, repository.ErrShopProductNotFound) {
		t.Errorf("expected ErrShopProductNotFound, got %v", err)
	}
}

func TestShopListFiltersByActivityAndRoles(t *testing.T) {
	db := testDB(t)
	repo := repository.NewShopRepository(db)
	ctx := context.Background()

	executorOnly := &repository.ShopProduct{
		Kind: repository.ShopKindPerk, Category: "perks",
		Title: map[string]interface{}{"ru": "x"}, Price: money.FromRubles(100),
		PerkKind: strPtr("COMMISSION_FREE"), PerkDays: intPtr(1),
		Roles: []string{"EXECUTOR"}, IsActive: true,
	}
	forAll := &repository.ShopProduct{
		Kind: repository.ShopKindPerk, Category: "perks",
		Title: map[string]interface{}{"ru": "y"}, Price: money.FromRubles(100),
		PerkKind: strPtr("COMMISSION_FREE"), PerkDays: intPtr(1),
		IsActive: true,
	}
	inactive := &repository.ShopProduct{
		Kind: repository.ShopKindPerk, Category: "perks",
		Title: map[string]interface{}{"ru": "z"}, Price: money.FromRubles(100),
		PerkKind: strPtr("COMMISSION_FREE"), PerkDays: intPtr(1),
		IsActive: false,
	}
	for _, p := range []*repository.ShopProduct{executorOnly, forAll, inactive} {
		if err := repo.UpsertProduct(ctx, p); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, p := range []*repository.ShopProduct{executorOnly, forAll, inactive} {
			_, _ = db.Exec(`DELETE FROM shop_products WHERE id = $1`, p.ID)
		}
	})

	// Витрина исполнителя: активные, свои и общие — без неактивного.
	list, err := repo.ListProducts(ctx, repository.ShopProductFilter{ActiveOnly: true, Roles: []string{"EXECUTOR"}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if shopProductByID(list, executorOnly.ID) == nil || shopProductByID(list, forAll.ID) == nil {
		t.Error("executor storefront should include his own and the shared product")
	}
	if shopProductByID(list, inactive.ID) != nil {
		t.Error("storefront must not include an inactive product")
	}

	// Витрина заказчика: товар чужой роли не попадает в список.
	list, err = repo.ListProducts(ctx, repository.ShopProductFilter{ActiveOnly: true, Roles: []string{"CUSTOMER"}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if shopProductByID(list, executorOnly.ID) != nil {
		t.Error("customer storefront must not include an executor-only product")
	}
	if shopProductByID(list, forAll.ID) == nil {
		t.Error("customer storefront should include the shared product")
	}

	// Админка без фильтров видит всё, включая неактивное.
	list, err = repo.ListProducts(ctx, repository.ShopProductFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if shopProductByID(list, inactive.ID) == nil {
		t.Error("admin list should include the inactive product")
	}
}

func TestShopStockComesFromTheSharedShelf(t *testing.T) {
	db := testDB(t)
	repo := repository.NewShopRepository(db)
	gifts := repository.NewGiftRepository(db)
	ctx := context.Background()

	zero := 0
	seedShopGift(t, db, "shop-empty-shirt", repository.GiftKindPhysical, &zero)
	seedShopGift(t, db, "shop-unlimited-shirt", repository.GiftKindPhysical, nil)
	seedShopGift(t, db, "shop-cert", repository.GiftKindCertificate, nil)

	empty := &repository.ShopProduct{Kind: repository.ShopKindPhysical, Category: "merch",
		Title: map[string]interface{}{"ru": "x"}, Price: 1, GiftCode: strPtr("shop-empty-shirt"), IsActive: true}
	unlimited := &repository.ShopProduct{Kind: repository.ShopKindPhysical, Category: "merch",
		Title: map[string]interface{}{"ru": "y"}, Price: 1, GiftCode: strPtr("shop-unlimited-shirt"), IsActive: true}
	cert := &repository.ShopProduct{Kind: repository.ShopKindCertificate, Category: "certs",
		Title: map[string]interface{}{"ru": "z"}, Price: 1, GiftCode: strPtr("shop-cert"), IsActive: true}
	for _, p := range []*repository.ShopProduct{empty, unlimited, cert} {
		if err := repo.UpsertProduct(ctx, p); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, p := range []*repository.ShopProduct{empty, unlimited, cert} {
			_, _ = db.Exec(`DELETE FROM shop_products WHERE id = $1`, p.ID)
		}
	})

	get := func(id uuid.UUID) *repository.ShopProduct {
		p, err := repo.GetProduct(ctx, id)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		return p
	}

	if p := get(empty.ID); p.InStock {
		t.Error("a shirt with zero stock must be out of stock")
	}
	if p := get(unlimited.ID); !p.InStock {
		t.Error("a NULL stock means unlimited, as with achievement gifts")
	}
	if p := get(cert.ID); p.InStock {
		t.Error("a certificate without free codes must be out of stock")
	}

	if _, err := gifts.AddCodes(ctx, "shop-cert", []string{"SECRET-1", "SECRET-2"}); err != nil {
		t.Fatalf("add codes: %v", err)
	}
	if p := get(cert.ID); !p.InStock || p.StockCount == nil || *p.StockCount != 2 {
		t.Errorf("cert after adding codes: in_stock=%v count=%v", p.InStock, p.StockCount)
	}
}

func TestShopLockProductReadsInsideTransaction(t *testing.T) {
	db := testDB(t)
	repo := repository.NewShopRepository(db)
	ctx := context.Background()

	p := &repository.ShopProduct{
		Kind: repository.ShopKindPerk, Category: "perks",
		Title: map[string]interface{}{"ru": "x"}, Price: money.FromRubles(100),
		PerkKind: strPtr("COMMISSION_FREE"), PerkDays: intPtr(1), IsActive: true,
	}
	if err := repo.UpsertProduct(ctx, p); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM shop_products WHERE id = $1`, p.ID) })

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	locked, err := repo.LockProduct(ctx, tx, p.ID)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	if locked.Price != p.Price {
		t.Errorf("locked price = %s, expected %s", locked.Price, p.Price)
	}
	if _, err := repo.LockProduct(ctx, tx, uuid.New()); !errors.Is(err, repository.ErrShopProductNotFound) {
		t.Errorf("expected ErrShopProductNotFound, got %v", err)
	}
}

func TestShopPickupPointsCRUD(t *testing.T) {
	db := testDB(t)
	repo := repository.NewShopRepository(db)
	ctx := context.Background()

	point := &repository.ShopPickupPoint{
		Title: map[string]interface{}{"ru": "Офис на Тверской"}, Address: "Москва, Тверская, 1",
		Hours: strPtr("10:00–20:00"), IsActive: true,
	}
	if err := repo.CreatePickupPoint(ctx, point); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM shop_pickup_points WHERE id = $1`, point.ID) })

	list, err := repo.ListPickupPoints(ctx, true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *repository.ShopPickupPoint
	for _, p := range list {
		if p.ID == point.ID {
			found = p
		}
	}
	if found == nil || found.Address != point.Address {
		t.Fatalf("created point not in the active list: %+v", found)
	}

	// Выключенный пункт не предлагается при оформлении, но остаётся в справочнике.
	point.IsActive = false
	if err := repo.UpdatePickupPoint(ctx, point); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, _ = repo.ListPickupPoints(ctx, true)
	for _, p := range list {
		if p.ID == point.ID {
			t.Error("a deactivated point must not be offered at checkout")
		}
	}
	list, _ = repo.ListPickupPoints(ctx, false)
	found = nil
	for _, p := range list {
		if p.ID == point.ID {
			found = p
		}
	}
	if found == nil {
		t.Error("a deactivated point must stay in the directory")
	}

	if err := repo.UpdatePickupPoint(ctx, &repository.ShopPickupPoint{ID: uuid.New(), Title: map[string]interface{}{"ru": "x"}}); !errors.Is(err, repository.ErrShopPickupPointNotFound) {
		t.Errorf("expected ErrShopPickupPointNotFound on update, got %v", err)
	}
	if err := repo.DeletePickupPoint(ctx, point.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := repo.DeletePickupPoint(ctx, point.ID); !errors.Is(err, repository.ErrShopPickupPointNotFound) {
		t.Errorf("expected ErrShopPickupPointNotFound on re-delete, got %v", err)
	}
}

func strPtr(s string) *string { return &s }
func intPtr(n int) *int       { return &n }
