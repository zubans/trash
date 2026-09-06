package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"healthlogin/backend/repository"
)

// Действия над разделом. Их четыре, и больше не нужно: раздел можно смотреть,
// заводить в нём новое, править существующее и удалять. Всё, что админ делает в
// панели, укладывается в одно из четырёх — «одобрить пополнение» это правка
// заявки, «разослать письмо» это создание рассылки.
const (
	ActionView   = "view"
	ActionCreate = "create"
	ActionEdit   = "edit"
	ActionDelete = "delete"
)

// PermissionSection — один раздел админки: пункт меню, маршрут фронтенда и
// набор действий, которые в нём вообще возможны. Каталог живёт здесь, а не в
// базе, потому что он обязан совпадать с тем, что реально охраняют маршруты:
// строка в базе может пережить удаление раздела, а константа рядом с
// маршрутом — нет.
type PermissionSection struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Group   string   `json:"group"`
	Route   string   `json:"route"`
	Actions []string `json:"actions"`
	// Hint объясняет, что именно открывает право, когда из названия раздела это
	// не очевидно (админ раздаёт права, глядя на этот список, а не на маршруты).
	Hint string `json:"hint,omitempty"`
}

// permissionCatalog перечисляет все разделы панели в порядке меню. Порядок
// значим: страница ролей рисует матрицу прав ровно в этом порядке, поэтому она
// читается как боковая панель.
var permissionCatalog = []PermissionSection{
	{Key: "users", Label: "Пользователи", Group: "Управление", Route: "/admin/users",
		Actions: []string{ActionView, ActionEdit},
		Hint:    "Правка — статус, верификация, роли, имя, адрес и пополнение баланса."},
	{Key: "roles", Label: "Роли и права", Group: "Управление", Route: "/admin/roles",
		Actions: []string{ActionView, ActionCreate, ActionEdit, ActionDelete},
		Hint:    "Кто может заводить роли и раздавать права. Выдавайте с осторожностью: обладатель этого права может выдать себе любое другое."},
	{Key: "support_chats", Label: "Чаты поддержки", Group: "Управление", Route: "/admin/support-chats",
		Actions: []string{ActionView, ActionEdit},
		Hint:    "Правка — бан и разбан собеседника в чате поддержки."},
	{Key: "topups", Label: "Пополнения", Group: "Управление", Route: "/admin/topups",
		Actions: []string{ActionView, ActionEdit},
		Hint:    "Правка — одобрение и отклонение заявок, то есть движение денег."},
	{Key: "withdrawals", Label: "Выводы", Group: "Управление", Route: "/admin/withdrawals",
		Actions: []string{ActionView, ActionEdit},
		Hint:    "Правка — одобрение и отклонение выплат."},
	{Key: "commission", Label: "Комиссия платформы", Group: "Управление", Route: "/admin/commission",
		Actions: []string{ActionView, ActionEdit},
		Hint:    "Правка — вывод накопленной комиссии."},
	{Key: "transactions", Label: "Проводки", Group: "Управление", Route: "/admin/transactions",
		Actions: []string{ActionView}},
	{Key: "reconciliation", Label: "Сверка", Group: "Управление", Route: "/admin/reconciliation",
		Actions: []string{ActionView}},
	{Key: "incidents", Label: "Денежные инциденты", Group: "Управление", Route: "/admin/incidents",
		Actions: []string{ActionView, ActionEdit},
		Hint:    "Правка — пометить инцидент разобранным."},
	{Key: "broadcasts", Label: "Рассылки", Group: "Управление", Route: "/admin/broadcasts",
		Actions: []string{ActionView, ActionCreate},
		Hint:    "Создание — отправка письма или внутренней почты списку получателей."},

	{Key: "shifts", Label: "Активные смены", Group: "Система", Route: "/admin/shifts",
		Actions: []string{ActionView}},
	{Key: "orders", Label: "Заказы", Group: "Система", Route: "/admin/orders/active",
		Actions: []string{ActionView},
		Hint:    "Активные и завершённые заказы."},
	{Key: "service_catalog", Label: "Конструктор услуг", Group: "Система", Route: "/admin/service-catalog",
		Actions: []string{ActionView, ActionCreate, ActionEdit, ActionDelete}},
	{Key: "achievements", Label: "Ачивки", Group: "Система", Route: "/admin/achievements",
		Actions: []string{ActionView, ActionCreate, ActionEdit, ActionDelete}},
	{Key: "gifts", Label: "Подарки", Group: "Система", Route: "/admin/gifts",
		Actions: []string{ActionView, ActionCreate, ActionEdit},
		Hint:    "Правка — в том числе погашение купона на пункте выдачи."},
	{Key: "escalations", Label: "Модерация проверок", Group: "Система", Route: "/admin/escalations",
		Actions: []string{ActionView, ActionEdit},
		Hint:    "Правка — решение по эскалации."},
	{Key: "settings", Label: "Настройки системы", Group: "Система", Route: "/admin/settings",
		Actions: []string{ActionView, ActionEdit}},
	{Key: "releases", Label: "Релизы приложения", Group: "Система", Route: "",
		Actions: []string{ActionCreate},
		Hint:    "Загрузка APK. Отдельного пункта меню нет."},
}

// PermissionCatalog отдаёт каталог разделов. Копия: вызывающий — обработчик,
// сериализующий его клиенту, и подменить общий каталог он не должен.
func PermissionCatalog() []PermissionSection {
	out := make([]PermissionSection, len(permissionCatalog))
	copy(out, permissionCatalog)
	return out
}

// Perm собирает код права из раздела и действия. Маршруты пишут его через эту
// функцию, а не строкой, чтобы опечатка стала ошибкой компиляции там, где
// раздела нет.
func Perm(section, action string) string { return section + "." + action }

// allPermissions — плоский набор всех кодов каталога. Считается один раз: он
// нужен и валидации, и суперпользователю, у которого «все права» это буквально
// этот список.
var allPermissions = func() map[string]struct{} {
	set := make(map[string]struct{})
	for _, s := range permissionCatalog {
		for _, a := range s.Actions {
			set[Perm(s.Key, a)] = struct{}{}
		}
	}
	return set
}()

// AllPermissions возвращает все коды прав каталога, в порядке разделов.
func AllPermissions() []string {
	out := make([]string, 0, len(allPermissions))
	for _, s := range permissionCatalog {
		for _, a := range s.Actions {
			out = append(out, Perm(s.Key, a))
		}
	}
	return out
}

// IsKnownPermission сообщает, есть ли такой код в каталоге. Права, которых нет,
// сохранять нельзя: они выглядели бы в интерфейсе выданными, но не охраняли бы
// ничего.
func IsKnownPermission(code string) bool {
	_, ok := allPermissions[code]
	return ok
}

// permissionCacheTTL — насколько долго разрешается держать карту «роль → права»
// без перечитывания. Она меняется только руками администратора на странице
// ролей, и та же операция сбрасывает кэш, поэтому TTL здесь — страховка от
// второго экземпляра процесса, а не основной механизм обновления.
const permissionCacheTTL = time.Minute

// Permissions отвечает на единственный вопрос: можно ли этому пользователю
// делать вот это. Ответ складывается из прав всех его ролей.
type Permissions struct {
	roles repository.RoleRepository

	mu      sync.RWMutex
	byRole  map[string][]string
	expires time.Time
}

// NewPermissions создаёт службу прав поверх справочника ролей.
func NewPermissions(roles repository.RoleRepository) *Permissions {
	return &Permissions{roles: roles}
}

// Invalidate роняет кэш. Вызывается всем, что меняет права роли.
func (p *Permissions) Invalidate() {
	p.mu.Lock()
	p.byRole = nil
	p.expires = time.Time{}
	p.mu.Unlock()
}

func (p *Permissions) snapshot(ctx context.Context) map[string][]string {
	p.mu.RLock()
	if p.byRole != nil && time.Now().Before(p.expires) {
		m := p.byRole
		p.mu.RUnlock()
		return m
	}
	p.mu.RUnlock()

	loaded, err := p.roles.PermissionsByRole(ctx)
	if err != nil {
		// База недоступна — отвечаем по последнему известному состоянию, если оно
		// есть. Пустая карта здесь означала бы «прав нет ни у кого», то есть
		// превращала бы сбой чтения в массовый отказ в доступе.
		p.mu.RLock()
		defer p.mu.RUnlock()
		return p.byRole
	}

	p.mu.Lock()
	p.byRole = loaded
	p.expires = time.Now().Add(permissionCacheTTL)
	p.mu.Unlock()
	return loaded
}

// Effective возвращает полный набор прав пользователя — объединение прав всех
// его ролей. У администратора это весь каталог: он суперпользователь по коду,
// поэтому право, добавленное новой версией, действует для него сразу.
func (p *Permissions) Effective(ctx context.Context, user *repository.User) []string {
	if user == nil {
		return nil
	}
	if user.HasRole(repository.RoleAdmin) {
		return AllPermissions()
	}

	byRole := p.snapshot(ctx)
	seen := make(map[string]struct{})
	var out []string
	for _, role := range userRoles(user) {
		for _, code := range byRole[role] {
			if _, dup := seen[code]; dup {
				continue
			}
			seen[code] = struct{}{}
			out = append(out, code)
		}
	}
	return out
}

// Can сообщает, есть ли у пользователя это право.
func (p *Permissions) Can(ctx context.Context, user *repository.User, permission string) bool {
	if user == nil {
		return false
	}
	if user.HasRole(repository.RoleAdmin) {
		return true
	}
	byRole := p.snapshot(ctx)
	for _, role := range userRoles(user) {
		for _, code := range byRole[role] {
			if code == permission {
				return true
			}
		}
	}
	return false
}

// CanAny сообщает, есть ли у пользователя хоть одно право в разделах панели.
// Именно это, а не роль ADMIN, открывает саму панель: роль «финансист» с одной
// галочкой «сверка» должна дойти до своей страницы.
func (p *Permissions) CanAny(ctx context.Context, user *repository.User) bool {
	if user == nil {
		return false
	}
	if user.HasRole(repository.RoleAdmin) {
		return true
	}
	byRole := p.snapshot(ctx)
	for _, role := range userRoles(user) {
		if len(byRole[role]) > 0 {
			return true
		}
	}
	return false
}

// userRoles отдаёт роли пользователя, откатываясь к основной, когда полный
// набор не загружен, — той же логикой, что и User.HasRole.
func userRoles(user *repository.User) []string {
	if len(user.Roles) > 0 {
		return user.Roles
	}
	if strings.TrimSpace(user.Role) == "" {
		return nil
	}
	return []string{user.Role}
}
