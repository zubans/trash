package repository_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/repository"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("skipping database test: DATABASE_URL not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("cannot ping test db: %v", err)
	}
	if err := repository.Migrate(db, "../migrations"); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	return db
}

func createTestUser(t *testing.T, db *sql.DB, role string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	phone := "+7" + uuid.New().String()[:10]
	_, err := db.Exec(`INSERT INTO users (id, role, phone, password, balance, status) VALUES ($1, $2, $3, 'hash', 1000, 'ACTIVE')`,
		id, role, phone)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}
	return id
}

func TestAddressRepository_CRUD(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := repository.NewAddressRepository(db)
	ctx := context.Background()

	userID := createTestUser(t, db, "CUSTOMER")

	// 1. Add first address (should become default automatically)
	lat1, lon1 := 55.7558, 37.6173
	addr1 := repository.Address{
		Address: "Россия, г. Москва, ул. Арбат, д. 10",
		City:    "Москва",
		Street:  "Арбат",
		House:   "10",
		Lat:     &lat1,
		Lon:     &lon1,
		Source:  "dadata",
	}

	list, err := repo.Add(ctx, userID, addr1)
	if err != nil {
		t.Fatalf("unexpected error adding first address: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 address, got %d", len(list))
	}
	if !list[0].IsDefault {
		t.Errorf("first address must be default")
	}
	if list[0].City != "Москва" {
		t.Errorf("expected city Москва, got %s", list[0].City)
	}

	// 2. Add second address
	lat2, lon2 := 55.7512, 37.6184
	addr2 := repository.Address{
		Address: "Россия, г. Москва, ул. Тверская, д. 5",
		City:    "Москва",
		Street:  "Тверская",
		House:   "5",
		Lat:     &lat2,
		Lon:     &lon2,
	}

	list, err = repo.Add(ctx, userID, addr2)
	if err != nil {
		t.Fatalf("unexpected error adding second address: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 addresses, got %d", len(list))
	}

	// 3. Attempting to add a third address must return ErrAddressLimitReached
	addr3 := repository.Address{Address: "Россия, г. Москва, ул. Ленина, д. 1"}
	_, err = repo.Add(ctx, userID, addr3)
	if err != repository.ErrAddressLimitReached {
		t.Fatalf("expected ErrAddressLimitReached, got %v", err)
	}

	// 4. Set second address as default
	var secondID uuid.UUID
	for _, a := range list {
		if a.Address == addr2.Address {
			secondID = a.ID
		}
	}
	list, err = repo.SetDefault(ctx, userID, secondID)
	if err != nil {
		t.Fatalf("unexpected error setting default: %v", err)
	}
	for _, a := range list {
		if a.ID == secondID && !a.IsDefault {
			t.Errorf("expected address %s to be default", secondID)
		}
	}

	// 5. Delete default address -> remaining address should become default
	list, err = repo.Delete(ctx, userID, secondID)
	if err != nil {
		t.Fatalf("unexpected error deleting address: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 remaining address, got %d", len(list))
	}
	if !list[0].IsDefault {
		t.Errorf("remaining address should become default after deleting previous default")
	}
}

// Re-saving an address the user already has is an update. It must not clear the
// default flag: doing so leaves an account holding addresses while having no
// default at all, and the profile then reports an empty address.
func TestAddressRepository_ResavingKeepsDefault(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := repository.NewAddressRepository(db)
	ctx := context.Background()
	userID := createTestUser(t, db, "CUSTOMER")

	line := "Россия, г. Москва, ул. Арбат, д. 10"
	if _, err := repo.Add(ctx, userID, repository.Address{Address: line, City: "Москва"}); err != nil {
		t.Fatalf("unexpected error adding address: %v", err)
	}

	// The same line again, this time carrying the coordinates that arrive with
	// a suggestion, and without asking to be the default.
	lat, lon := 55.7512, 37.6000
	list, err := repo.Add(ctx, userID, repository.Address{
		Address: line, City: "Москва", Lat: &lat, Lon: &lon,
	})
	if err != nil {
		t.Fatalf("unexpected error re-saving address: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("re-saving the same address must not create a second row, got %d", len(list))
	}
	if !list[0].IsDefault {
		t.Error("re-saving an address must not strip its default flag")
	}
	if list[0].Lat == nil || *list[0].Lat != lat {
		t.Error("re-saving an address should refresh its stored coordinates")
	}
}

// The limit counts addresses, so it may only reject a genuinely new one. A user
// holding the maximum must still be able to correct one they already have.
func TestAddressRepository_LimitAllowsUpdatingExisting(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := repository.NewAddressRepository(db)
	ctx := context.Background()
	userID := createTestUser(t, db, "CUSTOMER")

	first := "Россия, г. Москва, ул. Арбат, д. 10"
	second := "Россия, г. Москва, ул. Тверская, д. 5"
	if _, err := repo.Add(ctx, userID, repository.Address{Address: first}); err != nil {
		t.Fatalf("unexpected error adding first address: %v", err)
	}
	if _, err := repo.Add(ctx, userID, repository.Address{Address: second}); err != nil {
		t.Fatalf("unexpected error adding second address: %v", err)
	}

	// At the limit: a new address is refused ...
	if _, err := repo.Add(ctx, userID, repository.Address{Address: "Россия, г. Москва, ул. Ленина, д. 1"}); err != repository.ErrAddressLimitReached {
		t.Fatalf("expected ErrAddressLimitReached for a new address, got %v", err)
	}

	// ... but updating one that is already stored is not a new address.
	list, err := repo.Add(ctx, userID, repository.Address{Address: second, City: "Москва", House: "5"})
	if err != nil {
		t.Fatalf("updating an existing address at the limit must succeed, got %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected the address count to stay at 2, got %d", len(list))
	}
	for _, a := range list {
		if a.Address == second && a.City != "Москва" {
			t.Error("the update should have refreshed the stored parts")
		}
	}
}

// Adding with IsDefault promotes the address and demotes the previous default,
// which is how an admin correcting an address makes it the one orders use.
func TestAddressRepository_AddAsDefaultPromotes(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := repository.NewAddressRepository(db)
	ctx := context.Background()
	userID := createTestUser(t, db, "CUSTOMER")

	first := "Россия, г. Москва, ул. Арбат, д. 10"
	second := "Россия, г. Москва, ул. Тверская, д. 5"
	if _, err := repo.Add(ctx, userID, repository.Address{Address: first}); err != nil {
		t.Fatalf("unexpected error adding first address: %v", err)
	}

	list, err := repo.Add(ctx, userID, repository.Address{Address: second, IsDefault: true})
	if err != nil {
		t.Fatalf("unexpected error adding default address: %v", err)
	}

	var defaults int
	for _, a := range list {
		if a.IsDefault {
			defaults++
			if a.Address != second {
				t.Errorf("expected %q to be the default, got %q", second, a.Address)
			}
		}
	}
	if defaults != 1 {
		t.Errorf("exactly one address must be default, got %d", defaults)
	}
}
