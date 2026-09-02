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

	// 1. Добавляем первый адрес (должен автоматически стать адресом по умолчанию)
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

	// 2. Добавляем второй адрес
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

	// 3. Попытка добавить третий адрес должна вернуть ErrAddressLimitReached
	addr3 := repository.Address{Address: "Россия, г. Москва, ул. Ленина, д. 1"}
	_, err = repo.Add(ctx, userID, addr3)
	if err != repository.ErrAddressLimitReached {
		t.Fatalf("expected ErrAddressLimitReached, got %v", err)
	}

	// 4. Делаем второй адрес адресом по умолчанию
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

	// 5. Удаляем адрес по умолчанию -> оставшийся адрес должен стать умолчанием
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

// Повторное сохранение уже имеющегося адреса — это обновление. Оно не должно
// сбрасывать флаг умолчания: иначе учётка держит адреса, не имея ни одного
// адреса по умолчанию, и профиль тогда сообщает пустой адрес.
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

	// Та же строка ещё раз, теперь с координатами, приходящими с подсказкой,
	// и без просьбы стать адресом по умолчанию.
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

// Предел считает адреса, поэтому отклонять он может только по-настоящему новый.
// Пользователь на максимуме должен всё же уметь поправить уже имеющийся.
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

	// На пределе: новый адрес отвергается...
	if _, err := repo.Add(ctx, userID, repository.Address{Address: "Россия, г. Москва, ул. Ленина, д. 1"}); err != repository.ErrAddressLimitReached {
		t.Fatalf("expected ErrAddressLimitReached for a new address, got %v", err)
	}

	// ...но обновление уже сохранённого — не новый адрес.
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

// Добавление с IsDefault повышает адрес и снимает прежний по умолчанию —
// именно так админ, исправляющий адрес, делает его тем, по которому идут заказы.
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
