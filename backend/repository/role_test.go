package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/repository"
)

// Эти тесты идут к настоящей базе, потому что ловят они именно то, чего не
// видно в памяти: users.role и user_roles.role — разные столбцы, и до миграции
// 048 они были ещё и разных типов (перечисление role_type против text).
// Запрос, объединяющий их, компилировался и падал только в Postgres.

// Справочник ролей заводится миграцией, и роль, добавленная в него, назначается
// пользователю наравне с базовыми — включая ту, которой не было в старом
// перечислении role_type.
func TestRoleRepository_CustomRoleAssignable(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := repository.NewRoleRepository(db)
	ctx := context.Background()

	code := "TESTROLE" + uuid.New().String()[:4]
	code = sanitizeRoleCode(code)
	if err := repo.Create(ctx, &repository.Role{Code: code, Name: "Тестовая роль"}); err != nil {
		t.Fatalf("create role: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(ctx, code) })

	if err := repo.SetPermissions(ctx, code, []string{"reconciliation.view", "transactions.view"}); err != nil {
		t.Fatalf("set permissions: %v", err)
	}

	userID := createTestUser(t, db, "CUSTOMER")
	if err := repo.AssignUser(ctx, code, userID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	// Список носителей объединяет user_roles и users.role — ровно тот запрос,
	// который падал, пока основная роль была перечислением.
	users, total, err := repo.ListUsers(ctx, code, "", 20, 0)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if total != 1 || len(users) != 1 || users[0].ID != userID {
		t.Fatalf("носитель роли не найден: total=%d users=%v", total, users)
	}

	// Счётчик носителей в List считается тем же объединением.
	roles, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	var found *repository.Role
	for _, role := range roles {
		if role.Code == code {
			found = role
		}
	}
	if found == nil {
		t.Fatal("созданная роль не попала в справочник")
	}
	if found.UserCount != 1 {
		t.Fatalf("ожидался 1 носитель, получено %d", found.UserCount)
	}
	if len(found.Permissions) != 2 {
		t.Fatalf("ожидалось 2 права, получено %v", found.Permissions)
	}
}

// Основная роль пользователя может быть любой ролью справочника, а не только
// одной из трёх, что были в перечислении. Пользователь, у которого не осталось
// ролей, откатывается на заказчика, а не остаётся с несуществующей.
func TestRoleRepository_PrimaryRoleFollowsDirectory(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := repository.NewRoleRepository(db)
	users := repository.New(db)
	ctx := context.Background()

	code := sanitizeRoleCode("TMP" + uuid.New().String()[:5])
	if err := repo.Create(ctx, &repository.Role{Code: code, Name: "Временная роль"}); err != nil {
		t.Fatalf("create role: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(ctx, code) })

	userID := createTestUser(t, db, "CUSTOMER")

	// Основной ролью становится роль, которой в старом перечислении не было.
	if err := users.SetUserRoles(ctx, userID, []string{code}); err != nil {
		t.Fatalf("set user roles: %v", err)
	}
	user, err := users.FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.Role != code {
		t.Fatalf("основная роль должна быть %s, получено %s", code, user.Role)
	}

	// Удаление роли снимает её со всех и не оставляет основную роль указывающей
	// на то, чего больше нет.
	if err := repo.Delete(ctx, code); err != nil {
		t.Fatalf("delete role: %v", err)
	}
	user, err = users.FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("find user after delete: %v", err)
	}
	if user.Role != repository.RoleCustomer {
		t.Fatalf("после удаления роли основной должна стать %s, получено %s", repository.RoleCustomer, user.Role)
	}
}

// Системную роль репозиторий не удаляет, как бы её ни просили.
func TestRoleRepository_SystemRoleSurvivesDelete(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := repository.NewRoleRepository(db)
	ctx := context.Background()

	if err := repo.Delete(ctx, repository.RoleCustomer); err != repository.ErrRoleNotFound {
		t.Fatalf("ожидался отказ на удаление системной роли, получено %v", err)
	}
	if _, err := repo.Get(ctx, repository.RoleCustomer); err != nil {
		t.Fatalf("системная роль должна остаться: %v", err)
	}
}

// sanitizeRoleCode приводит случайный суффикс к тому, что принимает код роли:
// заглавные латинские буквы и цифры.
func sanitizeRoleCode(code string) string {
	out := make([]rune, 0, len(code))
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, r-32)
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			out = append(out, r)
		}
	}
	return string(out)
}
