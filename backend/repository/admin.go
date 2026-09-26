package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"
)

// Админские выборки по пользователям: список с фильтрами, адреса для рассылки,
// счётчик администраторов. Записью в users владеет UserRepository; здесь
// только чтение для панели.

// AdminUserRepository описывает чтение пользователей для панели.
type AdminUserRepository interface {
	// GetUsers — страница списка пользователей. role — любая роль справочника:
	// пользователь попадает в выборку, если роль есть в его наборе user_roles
	// (или она основная — для учёток старше наполнения user_roles).
	GetUsers(ctx context.Context, page, limit int, role, status, search string) ([]*User, int, error)
	// BroadcastEmails перечисляет адреса для рассылки по роли: подтверждённые,
	// а с includeUnverified — и те, по ссылке подтверждения которых не переходили.
	BroadcastEmails(ctx context.Context, role string, includeUnverified bool) ([]string, error)
	CountAdmins(ctx context.Context) (int, error)
}

// AdminRepository — прежнее имя AdminUserRepository. Оставлено, потому что на
// него ссылается RoleService (service/role.go); новый код пишет
// AdminUserRepository. Заявки на пополнение и вывод живут в PayoutRepository,
// журнал проводок — в TransactionJournalRepository, список заказов — в
// AdminOrderRepository, активные смены — в ShiftMonitorRepository.
type AdminRepository = AdminUserRepository

type adminUserRepo struct {
	db *sql.DB
}

// NewAdminUserRepository создаёт репозиторий админских выборок по пользователям.
func NewAdminUserRepository(db *sql.DB) AdminUserRepository {
	return &adminUserRepo{db: db}
}

func (r *adminUserRepo) GetUsers(ctx context.Context, page, limit int, role, status, search string) ([]*User, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	offset := (page - 1) * limit

	whereClause := "WHERE 1=1"
	var args []interface{}
	argCount := 1

	if role != "" {
		// Набор ролей пользователя — user_roles (миграция 039), и с миграции 048
		// в нём бывают роли справочника, которых в users.role нет ни у кого как
		// основной. Фильтр по одной колонке role показывал бы «исполнителей»
		// без тех, у кого исполнитель — вторая роль. users.role остаётся
		// запасным условием для учёток старше наполнения user_roles, как в
		// BroadcastEmails и RoleRepository.ListUsers.
		whereClause += fmt.Sprintf(
			" AND (EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = u.id AND ur.role = $%d) OR u.role = $%d)",
			argCount, argCount)
		args = append(args, role)
		argCount++
	}
	if status != "" {
		whereClause += fmt.Sprintf(" AND u.status = $%d", argCount)
		args = append(args, status)
		argCount++
	}
	if search != "" {
		whereClause += fmt.Sprintf(" AND u.phone LIKE $%d", argCount)
		args = append(args, "%"+search+"%")
		argCount++
	}

	// Общее число: таблица пользователей на порядки меньше проводок и заказов,
	// и список листается по номеру страницы, а не по offset, поэтому счётчик
	// нужен на каждой странице.
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM users u %s", whereClause)
	var total int
	err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Получаем постраничный список с адресом заказчика по умолчанию. Адрес теперь
	// живёт в единой таблице `addresses` (миграция 037 удалила
	// customer_profiles.address), поэтому читается оттуда. Набор мультиролей
	// читается тем же запросом, что и у FindBy*: отдельный «по мере возможности»
	// запрос молча оставлял Roles пустым при любом сбое.
	listQuery := fmt.Sprintf(
		`SELECT u.id, u.role, u.phone, u.balance, u.status, u.is_verified, u.created_at,
		        COALESCE((SELECT a.address FROM addresses a WHERE a.user_id = u.id AND a.is_default LIMIT 1), '') AS address,
		        COALESCE(u.last_name, ''), COALESCE(u.first_name, ''), COALESCE(u.patronymic, ''), u.birth_date,
		        COALESCE((SELECT array_agg(ur.role ORDER BY ur.role) FROM user_roles ur WHERE ur.user_id = u.id), '{}')
		 FROM users u
		 %s ORDER BY u.created_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, argCount, argCount+1,
	)
	queryArgs := append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, listQuery, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		var birthDate sql.NullTime
		var roles []string
		// Хеш пароля намеренно не выбирается: в админском списке он бесполезен и
		// вообще не должен путешествовать через приложение.
		err := rows.Scan(&u.ID, &u.Role, &u.Phone, &u.Balance, &u.Status, &u.Verified, &u.CreatedAt, &u.Address,
			&u.LastName, &u.FirstName, &u.Patronymic, &birthDate, pq.Array(&roles))
		if err != nil {
			return nil, 0, err
		}
		if birthDate.Valid {
			bd := birthDate.Time
			u.BirthDate = &bd
		}
		u.Roles = roles
		users = append(users, &u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// BroadcastEmails отдаёт подтверждённые адреса незаблокированных пользователей
// роли. Отдельный запрос, а не админский список: тот постраничен, не выбирает
// почту вовсе и смотрит только на основную роль, поэтому рассылка через него
// находила ноль получателей.
//
// Неподтверждённые адреса отдаются только по явной просьбе: регистрация пишет
// адрес сразу, но подтверждённым он становится лишь после перехода по ссылке,
// а в неподтверждённом бывает опечатка или чужой ящик.
func (r *adminUserRepo) BroadcastEmails(ctx context.Context, role string, includeUnverified bool) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT DISTINCT u.email
        FROM users u
        WHERE u.status::text <> 'BANNED'
          AND ($2 OR u.email_verified)
          AND COALESCE(u.email, '') <> ''
          AND (u.role = $1::text OR EXISTS (
              SELECT 1 FROM user_roles ur WHERE ur.user_id = u.id AND ur.role = $1::text))`, role, includeUnverified)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	emails := make([]string, 0)
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}

// CountAdmins используется, чтобы не дать понизить последнего администратора.
func (r *adminUserRepo) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'ADMIN'`).Scan(&count)
	return count, err
}
