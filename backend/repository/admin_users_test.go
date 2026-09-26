package repository_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/repository"
)

// Фильтр по роли смотрит в user_roles, а не только в users.role: заказчик, у
// которого исполнитель — вторая роль, попадает в список исполнителей, а роль
// справочника, которая ни у кого не основная, всё равно находит своих носителей.
func TestGetUsersFiltersByAnyHeldRole(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()

	users := repository.New(db)
	roles := repository.NewRoleRepository(db)
	admin := repository.NewAdminUserRepository(db)

	both := createTestUser(t, db, "CUSTOMER")
	onlyCustomer := createTestUser(t, db, "CUSTOMER")
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1, $2)`, both, onlyCustomer)
	})
	if err := users.SetUserRoles(ctx, both, []string{"CUSTOMER", "EXECUTOR"}); err != nil {
		t.Fatalf("set roles: %v", err)
	}

	code := "T" + strings.ToUpper(uuid.New().String()[:7])
	if err := roles.Create(ctx, &repository.Role{Code: code, Name: "Тестовая"}); err != nil {
		t.Fatalf("create role: %v", err)
	}
	t.Cleanup(func() { _ = roles.Delete(ctx, code) })
	if err := roles.AssignUser(ctx, code, both); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	has := func(list []*repository.User, id uuid.UUID) bool {
		for _, u := range list {
			if u.ID == id {
				return true
			}
		}
		return false
	}

	executors, _, err := admin.GetUsers(ctx, 1, 200, "EXECUTOR", "", "")
	if err != nil {
		t.Fatalf("filter EXECUTOR: %v", err)
	}
	if !has(executors, both) {
		t.Error("user with EXECUTOR as a secondary role is missing from the EXECUTOR filter")
	}
	if has(executors, onlyCustomer) {
		t.Error("plain customer listed under the EXECUTOR filter")
	}

	custom, total, err := admin.GetUsers(ctx, 1, 200, code, "", "")
	if err != nil {
		t.Fatalf("filter %s: %v", code, err)
	}
	if !has(custom, both) || total != 1 {
		t.Errorf("catalog role filter: total %d, holder found %v; want 1 and true", total, has(custom, both))
	}
	if u := custom[0]; len(u.Roles) != 3 {
		t.Errorf("listed user roles = %v, want all three", u.Roles)
	}
}
