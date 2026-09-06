package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// fakeRoleRepo — справочник ролей в памяти. Он хранит ровно то, что хранит
// таблица: роли, их права и назначения.
type fakeRoleRepo struct {
	roles   map[string]*repository.Role
	perms   map[string][]string
	holders map[string][]uuid.UUID
}

func newFakeRoleRepo() *fakeRoleRepo {
	return &fakeRoleRepo{
		roles: map[string]*repository.Role{
			repository.RoleCustomer: {Code: repository.RoleCustomer, Name: "Заказчик", IsSystem: true},
			repository.RoleAdmin:    {Code: repository.RoleAdmin, Name: "Администратор", IsSystem: true},
		},
		perms:   map[string][]string{},
		holders: map[string][]uuid.UUID{},
	}
}

func (f *fakeRoleRepo) List(ctx context.Context) ([]*repository.Role, error) {
	out := make([]*repository.Role, 0, len(f.roles))
	for code, role := range f.roles {
		copied := *role
		copied.Permissions = f.perms[code]
		copied.UserCount = len(f.holders[code])
		out = append(out, &copied)
	}
	return out, nil
}

func (f *fakeRoleRepo) Get(ctx context.Context, code string) (*repository.Role, error) {
	role, ok := f.roles[code]
	if !ok {
		return nil, repository.ErrRoleNotFound
	}
	copied := *role
	copied.Permissions = f.perms[code]
	return &copied, nil
}

func (f *fakeRoleRepo) Create(ctx context.Context, role *repository.Role) error {
	if _, exists := f.roles[role.Code]; exists {
		return repository.ErrRoleExists
	}
	stored := *role
	stored.CreatedAt = time.Now()
	f.roles[role.Code] = &stored
	return nil
}

func (f *fakeRoleRepo) Update(ctx context.Context, code, name, description string) error {
	role, ok := f.roles[code]
	if !ok {
		return repository.ErrRoleNotFound
	}
	role.Name, role.Description = name, description
	return nil
}

func (f *fakeRoleRepo) Delete(ctx context.Context, code string) error {
	role, ok := f.roles[code]
	if !ok || role.IsSystem {
		return repository.ErrRoleNotFound
	}
	delete(f.roles, code)
	delete(f.perms, code)
	delete(f.holders, code)
	return nil
}

func (f *fakeRoleRepo) SetPermissions(ctx context.Context, code string, permissions []string) error {
	if _, ok := f.roles[code]; !ok {
		return repository.ErrRoleNotFound
	}
	f.perms[code] = permissions
	return nil
}

func (f *fakeRoleRepo) PermissionsByRole(ctx context.Context) (map[string][]string, error) {
	out := make(map[string][]string, len(f.perms))
	for code, list := range f.perms {
		out[code] = append([]string(nil), list...)
	}
	return out, nil
}

func (f *fakeRoleRepo) ListUsers(ctx context.Context, code, search string, limit, offset int) ([]*repository.RoleUser, int, error) {
	ids := f.holders[code]
	users := make([]*repository.RoleUser, 0, len(ids))
	for _, id := range ids {
		users = append(users, &repository.RoleUser{ID: id, Phone: "79990000000"})
	}
	if offset >= len(users) {
		return nil, len(users), nil
	}
	end := offset + limit
	if end > len(users) {
		end = len(users)
	}
	return users[offset:end], len(users), nil
}

func (f *fakeRoleRepo) AssignUser(ctx context.Context, code string, userID uuid.UUID) error {
	for _, id := range f.holders[code] {
		if id == userID {
			return nil
		}
	}
	f.holders[code] = append(f.holders[code], userID)
	return nil
}

func (f *fakeRoleRepo) UnassignUser(ctx context.Context, code string, userID uuid.UUID) error {
	kept := f.holders[code][:0]
	for _, id := range f.holders[code] {
		if id != userID {
			kept = append(kept, id)
		}
	}
	f.holders[code] = kept
	return nil
}

func newRoleService(repo *fakeRoleRepo) *RoleService {
	return NewRoleService(repo, newMockUserRepo(), &mockAdminRepo{}, NewPermissions(repo))
}

// Каталог прав и то, что охраняет маршруты, — один список. Тест ловит
// опечатку в разделе: право, которого нет в каталоге, сохранять нельзя.
func TestPermissionCatalogIsClosed(t *testing.T) {
	if !IsKnownPermission("users.view") {
		t.Fatal("users.view должно быть известным правом")
	}
	if IsKnownPermission("users.launch") {
		t.Fatal("несуществующее действие не должно проходить как право")
	}
	for _, code := range AllPermissions() {
		if !strings.Contains(code, ".") {
			t.Fatalf("право %q не в форме «раздел.действие»", code)
		}
	}
}

// Роль видит ровно то, что ей выдали, — и ничего сверх.
func TestPermissionsFollowRoleGrants(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.roles["FINANCE"] = &repository.Role{Code: "FINANCE", Name: "Финансист"}
	repo.perms["FINANCE"] = []string{"reconciliation.view", "withdrawals.view"}
	permissions := NewPermissions(repo)

	user := &repository.User{ID: uuid.New(), Role: "FINANCE", Roles: []string{"FINANCE"}}
	ctx := context.Background()

	if !permissions.Can(ctx, user, "reconciliation.view") {
		t.Fatal("выданное право должно действовать")
	}
	if permissions.Can(ctx, user, "withdrawals.edit") {
		t.Fatal("невыданное право не должно действовать")
	}
	if !permissions.CanAny(ctx, user) {
		t.Fatal("роль с правами должна открывать панель")
	}

	// Пользователь без единого права не должен доходить до панели вообще.
	stranger := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Roles: []string{repository.RoleCustomer}}
	if permissions.CanAny(ctx, stranger) {
		t.Fatal("заказчик не должен открывать админскую панель")
	}
}

// Администратор — суперпользователь по коду: право, добавленное новой версией,
// действует для него сразу, без строки в базе.
func TestAdminKeepsEveryPermission(t *testing.T) {
	repo := newFakeRoleRepo()
	permissions := NewPermissions(repo)
	admin := &repository.User{ID: uuid.New(), Role: repository.RoleAdmin, Roles: []string{repository.RoleAdmin}}

	for _, code := range AllPermissions() {
		if !permissions.Can(context.Background(), admin, code) {
			t.Fatalf("администратор должен иметь право %s", code)
		}
	}
	if len(permissions.Effective(context.Background(), admin)) != len(AllPermissions()) {
		t.Fatal("действующие права администратора — весь каталог")
	}
}

// Права роли меняются — и это сразу видно, потому что тот же вызов сбрасывает кэш.
func TestRoleUpdateInvalidatesPermissionCache(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.roles["FINANCE"] = &repository.Role{Code: "FINANCE", Name: "Финансист"}
	svc := newRoleService(repo)
	ctx := context.Background()
	user := &repository.User{ID: uuid.New(), Role: "FINANCE", Roles: []string{"FINANCE"}}

	if svc.permissions.Can(ctx, user, "transactions.view") {
		t.Fatal("права ещё не выданы")
	}
	if _, err := svc.Update(ctx, uuid.New(), "FINANCE", "Финансист", "", []string{"transactions.view"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !svc.permissions.Can(ctx, user, "transactions.view") {
		t.Fatal("выданное право должно действовать без ожидания истечения кэша")
	}
}

// Несуществующее право не сохраняется: в матрице оно выглядело бы выданным, но
// не охраняло бы ни одного маршрута.
func TestUnknownPermissionRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	svc := newRoleService(repo)
	_, err := svc.Create(context.Background(), uuid.New(), "FINANCE", "Финансист", "", []string{"transactions.view", "users.launch"})
	if err == nil {
		t.Fatal("неизвестное право должно быть отвергнуто")
	}
	if _, getErr := repo.Get(context.Background(), "FINANCE"); !errors.Is(getErr, repository.ErrRoleNotFound) {
		t.Fatal("роль не должна остаться заведённой после отказа")
	}
}

// Код роли — то, что попадёт в users.role и в проверки прав, поэтому мусор в нём
// не принимается, а регистр приводится к верхнему.
func TestRoleCodeNormalizedAndValidated(t *testing.T) {
	repo := newFakeRoleRepo()
	svc := newRoleService(repo)
	ctx := context.Background()

	if _, err := svc.Create(ctx, uuid.New(), "поддержка", "Поддержка", "", nil); err == nil {
		t.Fatal("код не из латиницы должен быть отвергнут")
	}
	role, err := svc.Create(ctx, uuid.New(), " support ", "Поддержка", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if role.Code != "SUPPORT" {
		t.Fatalf("код должен нормализоваться к SUPPORT, получено %q", role.Code)
	}
}

// Системные роли неудаляемы: на них опираются маршруты и выбор дашборда.
func TestSystemRoleCannotBeDeleted(t *testing.T) {
	repo := newFakeRoleRepo()
	svc := newRoleService(repo)
	if err := svc.Delete(context.Background(), uuid.New(), repository.RoleCustomer); err == nil {
		t.Fatal("системную роль удалять нельзя")
	}
	if _, err := repo.Get(context.Background(), repository.RoleCustomer); err != nil {
		t.Fatal("системная роль должна остаться на месте")
	}
}

// Самого себя разжаловать нельзя — иначе панель остаётся без администратора.
func TestAdminCannotUnassignOwnAdminRole(t *testing.T) {
	repo := newFakeRoleRepo()
	svc := newRoleService(repo)
	actor := uuid.New()
	if err := svc.UnassignUser(context.Background(), actor, repository.RoleAdmin, actor); err == nil {
		t.Fatal("снятие роли администратора с самого себя должно быть отвергнуто")
	}
}

// Назначение и снятие роли отражаются в списке носителей — это и есть ответ на
// вопрос «кому подключена роль».
func TestAssignAndUnassignRole(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.roles["FINANCE"] = &repository.Role{Code: "FINANCE", Name: "Финансист"}
	svc := newRoleService(repo)
	ctx := context.Background()
	actor, user := uuid.New(), uuid.New()

	if err := svc.AssignUser(ctx, actor, "FINANCE", user); err != nil {
		t.Fatalf("assign: %v", err)
	}
	holders, total, err := svc.ListUsers(ctx, "FINANCE", "", 20, 0)
	if err != nil || total != 1 || len(holders) != 1 || holders[0].ID != user {
		t.Fatalf("носитель роли не найден: holders=%v total=%d err=%v", holders, total, err)
	}

	if err := svc.UnassignUser(ctx, actor, "FINANCE", user); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if _, total, _ := svc.ListUsers(ctx, "FINANCE", "", 20, 0); total != 0 {
		t.Fatal("после снятия роли носителей быть не должно")
	}
}

// Роль, которой нет в справочнике, назначить нельзя.
func TestAssignUnknownRoleRejected(t *testing.T) {
	svc := newRoleService(newFakeRoleRepo())
	if err := svc.AssignUser(context.Background(), uuid.New(), "GHOST", uuid.New()); !errors.Is(err, repository.ErrRoleNotFound) {
		t.Fatalf("ожидалась ErrRoleNotFound, получено %v", err)
	}
}
