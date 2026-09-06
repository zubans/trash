package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrRoleExists сообщает, что код роли уже занят.
var ErrRoleExists = errors.New("role with this code already exists")

// ErrRoleNotFound сообщает, что роли с таким кодом нет.
var ErrRoleNotFound = errors.New("role not found")

// Role — строка справочника ролей. Права хранятся отдельными строками и
// подгружаются вместе с ролью: список ролей в панели показывает и то, что роль
// открывает, и сколько человек её носит, потому что оба вопроса задаются про
// роль одновременно.
type Role struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// IsSystem отмечает роли, на которые опираются маршруты и дашборды
	// (CUSTOMER, EXECUTOR, MODERATOR, ADMIN). Их нельзя удалить.
	IsSystem    bool      `json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	Permissions []string  `json:"permissions"`
	UserCount   int       `json:"user_count"`
}

// RoleUser — одна строка списка «кому подключена роль».
type RoleUser struct {
	ID        uuid.UUID `json:"id"`
	Phone     string    `json:"phone"`
	Email     string    `json:"email"`
	FullName  string    `json:"full_name"`
	Status    string    `json:"status"`
	IsPrimary bool      `json:"is_primary"`
	CreatedAt time.Time `json:"created_at"`
}

// RoleRepository описывает хранение справочника ролей и их прав.
type RoleRepository interface {
	// List возвращает все роли с правами и числом носителей.
	List(ctx context.Context) ([]*Role, error)
	// Get возвращает одну роль с правами; ErrRoleNotFound, если её нет.
	Get(ctx context.Context, code string) (*Role, error)
	// Create заводит роль; ErrRoleExists, если код занят.
	Create(ctx context.Context, role *Role) error
	// Update меняет название и описание. Код роли неизменяем: на него ссылаются
	// строки user_roles и users.role.
	Update(ctx context.Context, code, name, description string) error
	// Delete удаляет роль вместе с её правами и назначениями (каскадом).
	Delete(ctx context.Context, code string) error
	// SetPermissions заменяет набор прав роли заданным.
	SetPermissions(ctx context.Context, code string, permissions []string) error
	// PermissionsByRole отдаёт карту «код роли → её права» одним запросом. Её
	// читает служба прав на каждом запросе (через кэш), поэтому она обязана быть
	// одним обращением к базе, а не одним на роль.
	PermissionsByRole(ctx context.Context) (map[string][]string, error)
	// ListUsers возвращает страницу носителей роли и их общее число.
	ListUsers(ctx context.Context, code, search string, limit, offset int) ([]*RoleUser, int, error)
	// AssignUser подключает роль пользователю. Повторное подключение — не ошибка.
	AssignUser(ctx context.Context, code string, userID uuid.UUID) error
	// UnassignUser снимает роль с пользователя и, если снятая роль была основной,
	// переводит users.role на любую из оставшихся.
	UnassignUser(ctx context.Context, code string, userID uuid.UUID) error
}

type roleRepo struct {
	db *sql.DB
}

// NewRoleRepository создаёт RoleRepository поверх соединения с базой.
func NewRoleRepository(db *sql.DB) RoleRepository {
	return &roleRepo{db: db}
}

func (r *roleRepo) List(ctx context.Context) ([]*Role, error) {
	// Счётчик носителей считается по user_roles с добавкой users.role: у
	// пользователя, заведённого до наполнения user_roles, основная роль может
	// не иметь там строки, и без UNION он не попал бы ни в один счёт.
	rows, err := r.db.QueryContext(ctx, `
		SELECT r.code, r.name, r.description, r.is_system, r.created_at,
		       COALESCE(c.cnt, 0)
		FROM roles r
		LEFT JOIN (
		    SELECT role, COUNT(*) AS cnt FROM (
		        SELECT user_id, role FROM user_roles
		        UNION
		        SELECT id, role FROM users
		    ) holders GROUP BY role
		) c ON c.role = r.code
		ORDER BY r.is_system DESC, r.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := make([]*Role, 0)
	index := make(map[string]*Role)
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.Code, &role.Name, &role.Description, &role.IsSystem, &role.CreatedAt, &role.UserCount); err != nil {
			return nil, err
		}
		role.Permissions = []string{}
		roles = append(roles, &role)
		index[role.Code] = &role
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	perms, err := r.PermissionsByRole(ctx)
	if err != nil {
		return nil, err
	}
	for code, list := range perms {
		if role, ok := index[code]; ok {
			role.Permissions = list
		}
	}
	return roles, nil
}

func (r *roleRepo) Get(ctx context.Context, code string) (*Role, error) {
	var role Role
	err := r.db.QueryRowContext(ctx,
		`SELECT code, name, description, is_system, created_at FROM roles WHERE code = $1`, code).
		Scan(&role.Code, &role.Name, &role.Description, &role.IsSystem, &role.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRoleNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT permission FROM role_permissions WHERE role_code = $1 ORDER BY permission`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	role.Permissions = []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		role.Permissions = append(role.Permissions, p)
	}
	return &role, rows.Err()
}

func (r *roleRepo) Create(ctx context.Context, role *Role) error {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO roles (code, name, description, is_system) VALUES ($1, $2, $3, false)
		 ON CONFLICT (code) DO NOTHING`,
		role.Code, role.Name, role.Description)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrRoleExists
	}
	return nil
}

func (r *roleRepo) Update(ctx context.Context, code, name, description string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE roles SET name = $2, description = $3 WHERE code = $1`, code, name, description)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrRoleNotFound
	}
	return nil
}

// Delete снимает роль отовсюду одной транзакцией. Строки user_roles уходят
// каскадом, но users.role — обычный текст без внешнего ключа, и оставленный там
// код удалённой роли сделал бы пользователя носителем несуществующей роли.
// Поэтому основная роль сначала переводится на любую из оставшихся у человека,
// а если не осталось ни одной — на заказчика, роль по умолчанию при регистрации.
func (r *roleRepo) Delete(ctx context.Context, code string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_roles WHERE role = $1`, code); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE users u
		SET role = COALESCE(
		    (SELECT ur.role FROM user_roles ur WHERE ur.user_id = u.id ORDER BY ur.role LIMIT 1),
		    $2)
		WHERE u.role = $1`, code, RoleCustomer); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM roles WHERE code = $1 AND is_system = false`, code)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrRoleNotFound
	}
	return tx.Commit()
}

func (r *roleRepo) SetPermissions(ctx context.Context, code string, permissions []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM roles WHERE code = $1)`, code).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrRoleNotFound
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_code = $1`, code); err != nil {
		return err
	}
	for _, permission := range permissions {
		if strings.TrimSpace(permission) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO role_permissions (role_code, permission) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			code, permission); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *roleRepo) PermissionsByRole(ctx context.Context) (map[string][]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT role_code, permission FROM role_permissions ORDER BY role_code, permission`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]string)
	for rows.Next() {
		var code, permission string
		if err := rows.Scan(&code, &permission); err != nil {
			return nil, err
		}
		out[code] = append(out[code], permission)
	}
	return out, rows.Err()
}

func (r *roleRepo) ListUsers(ctx context.Context, code, search string, limit, offset int) ([]*RoleUser, int, error) {
	if limit < 1 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	// Пустой поиск даёт «%%», под который подходит любая строка, поэтому
	// отдельной ветки «без фильтра» не нужно.
	pattern := "%" + strings.TrimSpace(search) + "%"

	// Носитель роли — тот, у кого есть строка в user_roles ИЛИ у кого она
	// основная. Второе условие держит в списке учётки старше наполнения
	// user_roles, для которых авторизация всё равно считает роль действующей.
	const where = `
		FROM users u
		WHERE (EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = u.id AND ur.role = $1) OR u.role = $1)
		  AND (u.phone ILIKE $2 OR COALESCE(u.email, '') ILIKE $2
		       OR COALESCE(u.last_name, '') || ' ' || COALESCE(u.first_name, '') ILIKE $2)`

	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) `+where, code, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT u.id, u.phone, COALESCE(u.email, ''),
		       TRIM(COALESCE(u.last_name, '') || ' ' || COALESCE(u.first_name, '') || ' ' || COALESCE(u.patronymic, '')),
		       u.status, (u.role = $1), u.created_at `+where+`
		ORDER BY u.created_at DESC
		LIMIT $3 OFFSET $4`, code, pattern, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	users := make([]*RoleUser, 0)
	for rows.Next() {
		var u RoleUser
		if err := rows.Scan(&u.ID, &u.Phone, &u.Email, &u.FullName, &u.Status, &u.IsPrimary, &u.CreatedAt); err != nil {
			return nil, 0, err
		}
		users = append(users, &u)
	}
	return users, total, rows.Err()
}

func (r *roleRepo) AssignUser(ctx context.Context, code string, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO user_roles (user_id, role) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, code)
	return err
}

// UnassignUser снимает роль. Как и Delete, он не оставляет users.role
// указывающей на снятое: основная роль переезжает на любую из оставшихся.
func (r *roleRepo) UnassignUser(ctx context.Context, code string, userID uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_roles WHERE user_id = $1 AND role = $2`, userID, code); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE users u
		SET role = COALESCE(
		    (SELECT ur.role FROM user_roles ur WHERE ur.user_id = u.id ORDER BY ur.role LIMIT 1),
		    $3)
		WHERE u.id = $1 AND u.role = $2`, userID, code, RoleCustomer); err != nil {
		return err
	}
	return tx.Commit()
}
