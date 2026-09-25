package repository

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"healthlogin/backend/money"
)

// Константы ролей. Основная роль пользователя живёт в users.role; полный набор,
// которым он обладает, — в user_roles (см. миграцию 039).
const (
	RoleCustomer  = "CUSTOMER"
	RoleExecutor  = "EXECUTOR"
	RoleModerator = "MODERATOR"
	RoleAdmin     = "ADMIN"
)

// Статусы учётной записи (users.status).
const (
	UserStatusActive = "ACTIVE"
	// UserStatusSoftBanned — аккаунт заблокирован, но войти можно: чтобы узнать
	// о блокировке, написать в поддержку и довести уже взятые заказы.
	UserStatusSoftBanned = "SOFT_BANNED"
	// UserStatusBanned — войти нельзя вовсе.
	UserStatusBanned = "BANNED"
)

// IsBlocked сообщает, закрыта ли пользователю новая работа: заказы, взятие
// заказов, ставки. Так ведут себя и BANNED, и SOFT_BANNED — различаются они
// только тем, пускает ли их аутентификация.
func (u *User) IsBlocked() bool {
	return u.Status == UserStatusBanned || u.Status == UserStatusSoftBanned
}

// User представляет запись пользователя в базе.
type User struct {
	ID            uuid.UUID  `json:"id"`
	Role          string     `json:"role"`
	Roles         []string   `json:"roles,omitempty"`
	Phone         string     `json:"phone"`
	Email         string     `json:"email"`
	LastName      string     `json:"last_name"`
	FirstName     string     `json:"first_name"`
	Patronymic    string     `json:"patronymic"`
	BirthDate     *time.Time `json:"birth_date,omitempty"`
	PendingEmail  string     `json:"pending_email,omitempty"`
	EmailVerified bool       `json:"email_verified"`
	Verified      bool       `json:"is_verified"`
	// Checked — «проверенный»: в системе есть паспорт с фото, и модератор его
	// просмотрел. Ставится только руками в админке.
	Checked bool `json:"is_checked"`
	// PDConsentVersion — принятая редакция согласия на обработку персональных
	// данных; nil — не принимал (зарегистрировался до галочки).
	PDConsentVersion       *int         `json:"pd_consent_version,omitempty"`
	PDConsentAt            *time.Time   `json:"pd_consent_at,omitempty"`
	EmailVerificationToken string       `json:"-"`
	EmailTokenExpiresAt    *time.Time   `json:"-"`
	PasswordResetCode      string       `json:"-"`
	PasswordResetExpiresAt *time.Time   `json:"-"`
	Password               string       `json:"-"` // bcrypt-хеш, управляется слоем сервисов
	Balance                money.Amount `json:"balance"`
	Status                 string       `json:"status"`
	CreatedAt              time.Time    `json:"created_at"`
	Address                string       `json:"address,omitempty"`
}

// BirthDateString отдаёт дату рождения в том виде, какого ждёт любой клиент, —
// YYYY-MM-DD, пусто, если не задана. Три места вызова форматировали её вручную.
func (u *User) BirthDateString() string {
	if u.BirthDate == nil {
		return ""
	}
	return u.BirthDate.Format("2006-01-02")
}

func (u *User) GetAge() int {
	if u.BirthDate == nil {
		return 0
	}
	now := time.Now()
	age := now.Year() - u.BirthDate.Year()
	if now.YearDay() < u.BirthDate.YearDay() {
		age--
	}
	return age
}

// IsVerified сообщает, верифицировал ли админ этого пользователя вручную. Флаг
// намеренно независим от EmailVerified: подтверждение почты доказывает владение
// адресом, а не доверие к учётной записи. Каждая проверка допуска — видимость
// заказов для заказчика и варианты услуг с requires_verification — читает этот
// единственный флаг.
func (u *User) IsVerified() bool {
	return u.Verified
}

// HasRole сообщает, обладает ли пользователь заданной ролью. Он смотрит в
// полный набор ролей, когда тот загружен, и всегда откатывается к основной
// роли, поэтому вызывающий, не загрузивший Roles, получает верный ответ по ней.
func (u *User) HasRole(role string) bool {
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return u.Role == role
}

// UserRepository описывает операции хранения пользователей.
type UserRepository interface {
	FindByPhone(ctx context.Context, phone string) (*User, error)
	// FindByEmail ищет без учёта регистра: адрес хранится в нижнем регистре
	// (миграция 066), а ввод приводится к нему здесь же.
	FindByEmail(ctx context.Context, email string) (*User, error)
	Create(ctx context.Context, user *User) error
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)
	// FindByIDs загружает набор пользователей с их ролями двумя запросами, а не
	// двумя на пользователя. Он существует ради списковых эндпоинтов, которым нужен
	// один пользователь на строку, чтобы ответить «может ли этот смотрящий видеть
	// этот заказ», и которые раньше спрашивали базу по разу на строку.
	// Несуществующие id просто отсутствуют в результате — отсутствующий
	// пользователь для фильтрующего список вызывающего нормальный исход, а не ошибка.
	FindByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*User, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	UpdateRole(ctx context.Context, id uuid.UUID, role string) error
	// SetUserRoles заменяет набор ролей пользователя заданным и держит users.role
	// (основную роль) указывающей на одну из них.
	SetUserRoles(ctx context.Context, id uuid.UUID, roles []string) error
	UpdateVerified(ctx context.Context, id uuid.UUID, verified bool) error
	// UpdateVerifiedTx — та же запись внутри транзакции вызывающего, для тех, кто
	// обязан закоммитить её вместе с доменным событием.
	UpdateVerifiedTx(ctx context.Context, q Querier, id uuid.UUID, verified bool) error
	CreateCustomerProfile(ctx context.Context, userID uuid.UUID, fullName string) error
	VerifyEmailToken(ctx context.Context, token string) (*User, error)
	UpdatePassword(ctx context.Context, userID uuid.UUID, newHashedPassword string) error
	SetPasswordResetCode(ctx context.Context, userID uuid.UUID, code string, expiresAt time.Time) error
	ResetPasswordWithCode(ctx context.Context, email, code, newHashedPassword string) (*User, error)
	// UpdateUserEmail записывает новый адрес как ожидающий подтверждения и
	// возвращает полную запись пользователя.
	UpdateUserEmail(ctx context.Context, userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*User, error)
	UpdateUserName(ctx context.Context, userID uuid.UUID, lastName, firstName, patronymic string) error
	UpdateUserBirthDate(ctx context.Context, userID uuid.UUID, birthDate time.Time) error
}

// repo реализует UserRepository поверх *sql.DB.
type repo struct {
	db *sql.DB
}

// New создаёт новый UserRepository поверх переданного соединения с базой.
func New(db *sql.DB) UserRepository {
	return &repo{db: db}
}

// userColumns — полный набор колонок пользователя в порядке, который читает
// scanUser. Таблица обязана идти под псевдонимом u. Последняя колонка — все
// роли из user_roles одним массивом: так любой FindBy* обходится одним
// обращением к базе, а не двумя.
//
// Пять рукописных списков, которые здесь были, разошлись: одни читали
// pending_email и согласие на ПД, другие нет, и пользователь, загруженный по
// телефону, отличался от загруженного по id. Теперь список один.
const userColumns = `u.id, u.role, u.phone, COALESCE(u.email, ''), COALESCE(u.last_name, ''),
	COALESCE(u.first_name, ''), COALESCE(u.patronymic, ''), u.birth_date, COALESCE(u.pending_email, ''),
	u.email_verified, u.is_verified, COALESCE(u.email_verification_token, ''),
	COALESCE(u.password_reset_code, ''), u.password_reset_expires_at, u.password, u.balance, u.status,
	u.created_at, u.is_checked, u.pd_consent_version, u.pd_consent_at,
	COALESCE((SELECT array_agg(ur.role ORDER BY ur.role) FROM user_roles ur WHERE ur.user_id = u.id), '{}')`

// scanUser читает пользователя из userColumns. Пользователь без строк в
// user_roles (появившийся до наполнения в миграции 039) получает набор из
// одной основной роли, чтобы HasRole отвечал по ней.
func scanUser(row rowScanner) (*User, error) {
	var u User
	var resetExp, birthDate sql.NullTime
	var roles []string
	if err := row.Scan(&u.ID, &u.Role, &u.Phone, &u.Email, &u.LastName, &u.FirstName, &u.Patronymic,
		&birthDate, &u.PendingEmail, &u.EmailVerified, &u.Verified, &u.EmailVerificationToken,
		&u.PasswordResetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt,
		&u.Checked, &u.PDConsentVersion, &u.PDConsentAt, pq.Array(&roles)); err != nil {
		return nil, err
	}
	if resetExp.Valid {
		u.PasswordResetExpiresAt = &resetExp.Time
	}
	if birthDate.Valid {
		u.BirthDate = &birthDate.Time
	}
	if len(roles) == 0 && u.Role != "" {
		roles = []string{u.Role}
	}
	u.Roles = roles
	return &u, nil
}

// FindByPhone ищет по точному номеру. Номера хранятся в канонической форме
// (миграция 026), и сервис приводит ввод к ней до вызова, поэтому здесь ровно
// одно условие по уникальному индексу — без REGEXP_REPLACE по всей таблице.
func (r *repo) FindByPhone(ctx context.Context, phone string) (*User, error) {
	return scanUser(r.db.QueryRowContext(ctx, findByPhoneSQL, phone))
}

const findByPhoneSQL = `SELECT ` + userColumns + ` FROM users u WHERE u.phone = $1`

// normalizeEmail — то, что хранится в users.email: без пробелов по краям и в
// нижнем регистре.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// FindByEmail ищет по LOWER(email): это выражение уникального индекса
// (миграция 066). Условие email <> ” повторяет предикат частичного индекса —
// без него планировщик не вправе им воспользоваться.
func (r *repo) FindByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(r.db.QueryRowContext(ctx, findByEmailSQL, normalizeEmail(email)))
}

const findByEmailSQL = `SELECT ` + userColumns + ` FROM users u WHERE LOWER(u.email) = $1 AND u.email <> ''`

func (r *repo) FindByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return scanUser(r.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users u WHERE u.id = $1`, id))
}

// FindByIDs загружает нескольких пользователей разом. Почему — см. интерфейс.
// Колонки те же, что у FindByID: пакетный вариант, вернувший более скудного
// пользователя, заставил бы предикат допуска вести себя по-разному в
// зависимости от того, каким путём загрузили его вход.
func (r *repo) FindByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*User, error) {
	result := make(map[uuid.UUID]*User, len(ids))
	placeholders, args := idList(ids)
	if len(args) == 0 {
		return result, nil
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT `+userColumns+` FROM users u WHERE u.id IN (`+placeholders+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		result[u.ID] = u
	}
	return result, rows.Err()
}

// SetUserRoles атомарно заменяет роли пользователя и перенаправляет users.role
// на одну из оставшихся ролей, когда текущей основной больше нет.
func (r *repo) SetUserRoles(ctx context.Context, id uuid.UUID, roles []string) error {
	return runInTx(ctx, r.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = $1`, id); err != nil {
			return err
		}
		clean := make([]string, 0, len(roles))
		for _, role := range roles {
			if role != "" {
				clean = append(clean, role)
			}
		}
		if len(clean) == 0 {
			return nil
		}
		// Одним оператором на весь набор, а не по строке на роль.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_roles (user_id, role) SELECT $1, unnest($2::text[]) ON CONFLICT DO NOTHING`,
			id, pq.Array(clean)); err != nil {
			return err
		}
		// Держим основную роль согласованной: если её убрали, берём первую из
		// нового набора, чтобы дашборд по умолчанию всё ещё разрешался.
		var current string
		if err := tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id = $1`, id).Scan(&current); err != nil {
			return err
		}
		for _, role := range clean {
			if role == current {
				return nil
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE users SET role = $1 WHERE id = $2`, clean[0], id)
		return err
	})
}

func (r *repo) Create(ctx context.Context, user *User) error {
	id := user.ID
	if id == uuid.Nil {
		id = uuid.New()
		user.ID = id
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, role, phone, email, last_name, first_name, patronymic, birth_date, pending_email, email_verified, is_verified, email_verification_token, email_token_expires_at, password, balance, status, created_at, pd_consent_version, pd_consent_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`,
		id, user.Role, user.Phone, normalizeEmail(user.Email), user.LastName, user.FirstName, user.Patronymic, user.BirthDate, user.PendingEmail, user.EmailVerified, user.Verified, user.EmailVerificationToken, user.EmailTokenExpiresAt, user.Password, user.Balance, user.Status, time.Now(), user.PDConsentVersion, user.PDConsentAt,
	)
	if err != nil {
		return err
	}
	// Зеркалим основную роль в user_roles, чтобы мультиролевая таблица была
	// авторитетным набором с момента существования учётной записи.
	if user.Role != "" {
		if _, err := r.db.ExecContext(ctx,
			`INSERT INTO user_roles (user_id, role) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, user.Role); err != nil {
			return err
		}
	}
	return nil
}

func (r *repo) VerifyEmailToken(ctx context.Context, token string) (*User, error) {
	var userID uuid.UUID
	err := r.db.QueryRowContext(ctx,
		`UPDATE users
		 SET email = COALESCE(NULLIF(pending_email, ''), email),
		     pending_email = NULL,
		     email_verified = true,
		     email_verification_token = NULL,
		     email_token_expires_at = NULL
		 WHERE email_verification_token = $1 AND (email_token_expires_at IS NULL OR email_token_expires_at > now())
		 RETURNING id`,
		token,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			var isExpired bool
			errExp := r.db.QueryRowContext(ctx,
				`SELECT EXISTS(SELECT 1 FROM users WHERE email_verification_token = $1 AND email_token_expires_at <= now())`,
				token,
			).Scan(&isExpired)
			if errExp == nil && isExpired {
				return nil, errors.New("verification_token_expired")
			}
			return nil, errors.New("invalid or expired verification token (valid 60m)")
		}
		return nil, err
	}
	return r.FindByID(ctx, userID)
}

// UpdatePassword заменяет сохранённый хеш и очищает любой ожидающий код сброса,
// чтобы код, выданный до изменения, нельзя было использовать после.
func (r *repo) UpdatePassword(ctx context.Context, userID uuid.UUID, newHashedPassword string) error {
	return execExpectingOne(ctx, r.db,
		`UPDATE users SET password = $1, password_reset_code = NULL,
		    password_reset_expires_at = NULL, password_reset_attempts = 0
		 WHERE id = $2`,
		newHashedPassword, userID)
}

func (r *repo) SetPasswordResetCode(ctx context.Context, userID uuid.UUID, code string, expiresAt time.Time) error {
	// Свежий код обнуляет счётчик попыток.
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET password_reset_code = $1, password_reset_expires_at = $2, password_reset_attempts = 0 WHERE id = $3`,
		code, expiresAt, userID,
	)
	return err
}

// maxResetAttempts ограничивает, сколько кодов можно попробовать на один запрос
// сброса. Без него числовой код просто перебирается внутри срока его действия.
const maxResetAttempts = 5

// ResetPasswordWithCode проверяет код под блокировкой строки и считает неудачные
// попытки, обнуляя код по достижении предела.
func (r *repo) ResetPasswordWithCode(ctx context.Context, email, code, newHashedPassword string) (*User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var (
		userID     uuid.UUID
		storedCode sql.NullString
		expiresAt  sql.NullTime
		attempts   int
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id, password_reset_code, password_reset_expires_at, COALESCE(password_reset_attempts, 0)
		 FROM users WHERE LOWER(email) = $1 AND email <> '' FOR UPDATE`,
		normalizeEmail(email),
	).Scan(&userID, &storedCode, &expiresAt, &attempts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("неверный или истекший код сброса")
		}
		return nil, err
	}

	invalidate := func() error {
		_, err := tx.ExecContext(ctx,
			`UPDATE users SET password_reset_code = NULL, password_reset_expires_at = NULL, password_reset_attempts = 0 WHERE id = $1`,
			userID,
		)
		return err
	}

	if !storedCode.Valid || storedCode.String == "" || !expiresAt.Valid || expiresAt.Time.Before(time.Now()) {
		return nil, errors.New("неверный или истекший код сброса")
	}

	if attempts >= maxResetAttempts {
		if err := invalidate(); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, errors.New("превышено число попыток, запросите новый код")
	}

	if subtle.ConstantTimeCompare([]byte(storedCode.String), []byte(code)) != 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_reset_attempts = COALESCE(password_reset_attempts, 0) + 1 WHERE id = $1`, userID); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, errors.New("неверный или истекший код сброса")
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE users
		 SET password = $1, password_reset_code = NULL, password_reset_expires_at = NULL, password_reset_attempts = 0
		 WHERE id = $2`,
		newHashedPassword, userID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, userID)
}

func (r *repo) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET status = $1 WHERE id = $2`, status, id)
	return err
}

// UpdateRole — легаси-сеттер одной роли: он делает заданную роль единственной
// ролью пользователя, держа user_roles (мультиролевой источник истины) в такт,
// чтобы эти двое никогда не разъезжались.
func (r *repo) UpdateRole(ctx context.Context, id uuid.UUID, role string) error {
	return r.SetUserRoles(ctx, id, []string{role})
}

// UpdateVerified выставляет флаг ручной верификации на собственном соединении.
func (r *repo) UpdateVerified(ctx context.Context, id uuid.UUID, verified bool) error {
	return r.UpdateVerifiedTx(ctx, nil, id, verified)
}

// UpdateVerifiedTx выставляет флаг внутри транзакции вызывающего. Такой нужен
// обоим писателям users.is_verified: админский эндпоинт публикует вместе с
// изменением событие user.verified, а применитель поведений выставляет флаг
// заодно с закрытием заказа и оплатой проверяющему. Флаг без своего события или
// событие без флага — ровно тот разрыв, который outbox и существует
// предотвращать.
func (r *repo) UpdateVerifiedTx(ctx context.Context, q Querier, id uuid.UUID, verified bool) error {
	_, err := exec(r.db, q).ExecContext(ctx, `UPDATE users SET is_verified = $1 WHERE id = $2`, verified, id)
	return err
}

func (r *repo) CreateCustomerProfile(ctx context.Context, userID uuid.UUID, fullName string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO customer_profiles (user_id, full_name)
		 VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET full_name = $2`,
		userID, fullName,
	)
	return err
}

// UpdateUserEmail записывает запрошенный адрес как ожидающий и отправляет
// пользователю ссылку подтверждения. Текущий адрес остаётся на месте, пока
// новый не подтверждён: запись сразу в email означала, что неподтверждённый
// адрес немедленно становился адресом учётки, а это позволяло занять адрес,
// который его настоящий владелец ещё не зарегистрировал, и роняло рабочий адрес
// пользователя, сделавшего опечатку.
//
// Адрес пишется в нижнем регистре: после подтверждения он станет users.email,
// а тот уникален по LOWER(email).
func (r *repo) UpdateUserEmail(ctx context.Context, userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*User, error) {
	var id uuid.UUID
	if err := r.db.QueryRowContext(ctx,
		`UPDATE users
		 SET pending_email = $1, email_verification_token = $2, email_token_expires_at = $3
		 WHERE id = $4
		 RETURNING id`,
		normalizeEmail(email), verificationToken, expiresAt, userID,
	).Scan(&id); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

func (r *repo) UpdateUserName(ctx context.Context, userID uuid.UUID, lastName, firstName, patronymic string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET last_name = $1, first_name = $2, patronymic = $3 WHERE id = $4`,
		lastName, firstName, patronymic, userID,
	)
	return err
}

func (r *repo) UpdateUserBirthDate(ctx context.Context, userID uuid.UUID, birthDate time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET birth_date = $1 WHERE id = $2`,
		birthDate, userID,
	)
	return err
}
