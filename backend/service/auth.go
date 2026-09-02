package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"healthlogin/backend/repository"
)

// AuthService занимается регистрацией и аутентификацией пользователей.
type AuthService struct {
	repo        repository.UserRepository
	addressRepo repository.AddressRepository
	execGeoRepo repository.ExecutorGeoRepository
	refreshRepo repository.RefreshTokenRepository
	tokenRepo   repository.TokenRepository
	resolver    AddressResolver
	mailer      MailSender
	secret      []byte
}

// JWTClaims содержит данные, извлечённые из проверенного access-токена.
type JWTClaims struct {
	UserID uuid.UUID
	Phone  string
	Role   string
}

// NewAuthService создаёт AuthService на переданном репозитории.
// Секрет подписи JWT читается из JWT_SECRET; если переменная не задана,
// используется значение по умолчанию для разработки.
func NewAuthService(repo repository.UserRepository, resolver AddressResolver) *AuthService {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret-change-me"
	}
	return NewAuthServiceWithSecret(repo, secret, resolver, NewSmtpMailSender())
}

// NewAuthServiceWithSecret создаёт AuthService с явным секретом JWT. Полезно
// для тестов и для окружений, куда секрет внедряют напрямую.
func NewAuthServiceWithSecret(repo repository.UserRepository, secret string, resolver AddressResolver, mailer MailSender) *AuthService {
	if mailer == nil {
		mailer = NewSmtpMailSender()
	}
	return &AuthService{repo: repo, resolver: resolver, mailer: mailer, secret: []byte(secret)}
}

// WithAddresses присоединяет репозиторий адресов, используемый при регистрации.
func (s *AuthService) WithAddresses(addressRepo repository.AddressRepository) *AuthService {
	s.addressRepo = addressRepo
	return s
}

// WithExecutorGeo присоединяет гео-репозиторий исполнителей для задания начального местоположения.
func (s *AuthService) WithExecutorGeo(execGeoRepo repository.ExecutorGeoRepository) *AuthService {
	s.execGeoRepo = execGeoRepo
	return s
}

// WithSessionStorage присоединяет хранилища, обслуживающие сессии: refresh-токены
// и чёрный список access-токенов. Без них сервис всё равно умеет выдавать
// access-токены — на это и опираются юнит-тесты.
func (s *AuthService) WithSessionStorage(refreshRepo repository.RefreshTokenRepository, tokenRepo repository.TokenRepository) *AuthService {
	s.refreshRepo = refreshRepo
	s.tokenRepo = tokenRepo
	return s
}

// minPasswordLength — самый короткий пароль, принимаемый при регистрации и при
// сбросе пароля.
const minPasswordLength = 8

// weakPasswords — значения, чаще всего встречающиеся в списках для подстановки учётных данных.
var weakPasswords = map[string]bool{
	"12345678": true, "123456789": true, "1234567890": true, "password": true,
	"qwerty123": true, "qwertyui": true, "11111111": true, "iloveyou": true,
	"admin123": true, "parol123": true, "password1": true,
}

// validatePassword требует минимальной стойкости. Без него принимался пароль из
// одного символа, что делало (неограниченный) эндпоинт входа тривиальным.
func validatePassword(password string) error {
	if len([]rune(password)) < minPasswordLength {
		return fmt.Errorf("пароль должен быть не короче %d символов", minPasswordLength)
	}
	if weakPasswords[strings.ToLower(password)] {
		return errors.New("этот пароль слишком простой, выберите другой")
	}
	return nil
}

// maxHumanAge ограничивает, насколько давней может быть дата рождения. Это
// проверка здравого смысла для набранной вручную даты, а не политика:
// реально что-то ограничивающие возрастные пределы живут по услугам в min_age.
const maxHumanAge = 120

// parseBirthDate превращает формат обмена в дату и отвергает то, что не может
// описывать живого человека. Настоящий вред наносит дата в будущем: GetAge
// сообщил бы отрицательный возраст, поэтому любая проверка min_age читалась бы
// как «слишком молод», и ни один экран не сказал бы почему.
func parseBirthDate(birthDate string) (time.Time, error) {
	birthDate = strings.TrimSpace(birthDate)
	if birthDate == "" {
		return time.Time{}, errors.New("укажите дату рождения")
	}
	t, err := time.Parse("2006-01-02", birthDate)
	if err != nil {
		return time.Time{}, errors.New("неверный формат даты рождения, ожидается ГГГГ-ММ-ДД")
	}
	now := time.Now()
	if t.After(now) {
		return time.Time{}, errors.New("дата рождения не может быть в будущем")
	}
	if t.Before(now.AddDate(-maxHumanAge, 0, 0)) {
		return time.Time{}, fmt.Errorf("дата рождения не может быть раньше, чем %d лет назад", maxHumanAge)
	}
	return t, nil
}

var phoneCleanup = regexp.MustCompile(`[^0-9+]`)

// normalizePhone приводит российский номер телефона к единой канонической
// форме, чтобы «+7 999 …», «8999…» и «7999…» не превратились в три отдельные
// учётные записи одного человека.
func normalizePhone(phone string) string {
	digits := phoneCleanup.ReplaceAllString(strings.TrimSpace(phone), "")
	digits = strings.TrimPrefix(digits, "+")
	switch {
	case len(digits) == 11 && strings.HasPrefix(digits, "8"):
		digits = "7" + digits[1:]
	case len(digits) == 10:
		digits = "7" + digits
	}
	return "+" + digits
}

// validRegistrationRole сообщает, можно ли выбрать роль при регистрации.
// ADMIN запрещён явно; разрешены только CUSTOMER и EXECUTOR.
func validRegistrationRole(role string) bool {
	return role == "CUSTOMER" || role == "EXECUTOR"
}

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// Register создаёт нового пользователя с указанными телефоном, почтой, паролем, датой рождения, адресом подачи и ролью.
func (s *AuthService) Register(ctx context.Context, phone, email, password, lastName, firstName, patronymic, birthDate, address, role string) (*repository.User, error) {
	return s.RegisterWithCoordinates(ctx, phone, email, password, lastName, firstName, patronymic, birthDate, address, role, nil, nil)
}

// RegisterWithCoordinates создаёт нового пользователя с почтой, телефоном, паролем, датой рождения и адресом.
func (s *AuthService) RegisterWithCoordinates(ctx context.Context, phone, email, password, lastName, firstName, patronymic, birthDate, address, role string, lat, lon *float64) (*repository.User, error) {
	if phone == "" || password == "" {
		return nil, errors.New("phone and password are required")
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	phone = normalizePhone(phone)
	lastName = strings.TrimSpace(lastName)
	firstName = strings.TrimSpace(firstName)
	patronymic = strings.TrimSpace(patronymic)
	if lastName == "" || firstName == "" || patronymic == "" {
		return nil, errors.New("last_name, first_name, and patronymic are required")
	}
	// Дальше обязательно: проверки min_age по услугам читают GetAge(), а учётка без
	// даты рождения читается как возраст 0 — молча недопущенная до любой услуги с
	// возрастным ограничением.
	parsedBirthDate, err := parseBirthDate(birthDate)
	if err != nil {
		return nil, err
	}
	email = strings.TrimSpace(email)
	if email == "" || !emailRegex.MatchString(email) {
		return nil, errors.New("a valid email is required")
	}
	if !validRegistrationRole(role) {
		return nil, errors.New("invalid role: must be CUSTOMER or EXECUTOR")
	}
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("address is required")
	}

	// Адрес проверяется на то, что он обязан содержать, — населённый пункт,
	// улицу и здание, — а не на совпадение с фиксированным написанием.
	// Старая проверка формата требовала чисто числового номера дома, поэтому
	// человек, живущий в доме 12к1, вообще не мог зарегистрироваться.
	parsedAddress := ParseAddressLine(address)
	if err := parsedAddress.Validate(); err != nil {
		return nil, err
	}
	normalizedAddress := parsedAddress.Compose()

	existingPhone, err := s.repo.FindByPhone(ctx, phone)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existingPhone != nil {
		return nil, errors.New("user with this phone already exists")
	}

	existingEmail, err := s.repo.FindByEmail(ctx, email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existingEmail != nil {
		return nil, errors.New("user with this email already exists")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	verificationToken := uuid.New().String()
	tokenExpiresAt := time.Now().Add(60 * time.Minute)

	user := &repository.User{
		Role:                   role,
		Phone:                  phone,
		Email:                  email,
		LastName:               lastName,
		FirstName:              firstName,
		Patronymic:             patronymic,
		BirthDate:              &parsedBirthDate,
		EmailVerified:          false,
		EmailVerificationToken: verificationToken,
		EmailTokenExpiresAt:    &tokenExpiresAt,
		Password:               string(hash),
		Balance:                0,
		Status:                 "ACTIVE",
	}
	if err := s.repo.Create(ctx, user); err != nil {
		return nil, err
	}

	created, err := s.repo.FindByPhone(ctx, phone)
	if err != nil {
		return nil, err
	}

	// Разрешаем координаты начального адреса, если они не переданы
	var resLat, resLon *float64
	if lat != nil && lon != nil {
		resLat = lat
		resLon = lon
	} else if s.resolver != nil {
		if geo, err := s.resolver.Resolve(ctx, normalizedAddress); err == nil && geo != nil {
			l := geo.Lat
			ln := geo.Lon
			resLat = &l
			resLon = &ln
		}
	}

	fullName := strings.TrimSpace(fmt.Sprintf("%s %s %s", lastName, firstName, patronymic))
	if role == "CUSTOMER" {
		if err := s.repo.CreateCustomerProfile(ctx, created.ID, fullName); err != nil {
			return nil, err
		}
	}

	if s.addressRepo != nil {
		addrRecord := parsedAddress.ToRecord()
		addrRecord.UserID = created.ID
		addrRecord.IsDefault = true
		if resLat != nil && resLon != nil {
			addrRecord.Lat = resLat
			addrRecord.Lon = resLon
		}
		if _, err := s.addressRepo.Add(ctx, created.ID, addrRecord); err != nil {
			log.Printf("[AuthService] failed to save initial address for user %s: %v", created.ID, err)
		}
	}

	// Задаём начальное местоположение исполнителя
	if role == "EXECUTOR" && resLat != nil && resLon != nil && s.execGeoRepo != nil {
		if err := s.execGeoRepo.UpdateExecutorLocation(ctx, created.ID, *resLat, *resLon, false); err != nil {
			log.Printf("[AuthService] failed to store initial executor geo for %s: %v", created.ID, err)
		}
	}

	if s.mailer != nil {
		_ = s.mailer.SendEmailVerification(email, verificationToken)
	}

	return created, nil
}

// Authenticate проверяет пару телефон/пароль или почта/пароль и возвращает подходящего пользователя.
func (s *AuthService) Authenticate(ctx context.Context, phoneOrEmail, password string) (*repository.User, error) {
	if phoneOrEmail == "" || password == "" {
		return nil, errors.New("phone and password are required")
	}

	input := strings.TrimSpace(phoneOrEmail)
	var user *repository.User
	var err error

	if strings.Contains(input, "@") {
		user, err = s.repo.FindByEmail(ctx, input)
	} else {
		user, err = s.repo.FindByPhone(ctx, normalizePhone(input))
		if (err != nil || user == nil) && input != "" {
			user, err = s.repo.FindByPhone(ctx, input)
		}
	}

	if err != nil || user == nil {
		// Хешируем в любом случае, чтобы отсутствующую учётку нельзя было отличить по времени.
		_, _ = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		return nil, errors.New("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, errors.New("invalid credentials")
	}

	return user, nil
}

// GenerateJWT создаёт подписанный JWT для аутентифицированного пользователя.
func (s *AuthService) GenerateJWT(ctx context.Context, user *repository.User) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   user.ID.String(),
		"phone": user.Phone,
		"role":  user.Role,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(accessTokenTTL).Unix(),
	})
	return token.SignedString(s.secret)
}

// ParseJWT проверяет строку токена и возвращает извлечённые утверждения.
func (s *AuthService) ParseJWT(ctx context.Context, tokenStr string) (*JWTClaims, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}

	sub, ok := claims["sub"].(string)
	if !ok {
		return nil, errors.New("missing sub claim")
	}
	userID, err := uuid.Parse(sub)
	if err != nil {
		return nil, err
	}

	phone, _ := claims["phone"].(string)
	role, _ := claims["role"].(string)

	return &JWTClaims{
		UserID: userID,
		Phone:  phone,
		Role:   role,
	}, nil
}

// VerifyEmail подтверждает почту пользователя по токену.
func (s *AuthService) VerifyEmail(ctx context.Context, token string) (*repository.User, error) {
	if token == "" {
		return nil, errors.New("token is required")
	}
	return s.repo.VerifyEmailToken(ctx, token)
}

// RequestPasswordReset генерирует 6-значный код сброса пароля и отправляет его письмом.
func (s *AuthService) RequestPasswordReset(ctx context.Context, email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return errors.New("укажите Email")
	}
	user, err := s.repo.FindByEmail(ctx, email)
	if err != nil || user == nil {
		// Сообщаем об успехе в любом случае: иной ответ здесь подсказал бы
		// атакующему, за какими адресами почты есть учётная запись.
		log.Printf("[PASSWORD RESET] requested for unknown email")
		return nil
	}

	// Криптостойкий 8-значный код сброса. Никакого отката ко времени:
	// предсказуемый код хуже, чем неудавшийся запрос.
	n, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return errors.New("не удалось сгенерировать код, попробуйте позже")
	}
	code := fmt.Sprintf("%08d", n.Int64())
	expiresAt := time.Now().Add(30 * time.Minute)

	if err := s.repo.SetPasswordResetCode(ctx, user.ID, code, expiresAt); err != nil {
		return err
	}

	if s.mailer != nil {
		if err := s.mailer.SendPasswordResetCode(email, code); err != nil {
			// Ответ не должен зависеть от того, есть ли за адресом учётная запись.
			// Возврат ошибки здесь, тогда как неизвестный адрес получает бодрый
			// успех, превращает этот эндпоинт в оракул существования учёток —
			// ровно то, чего избегает ветка неизвестного адреса выше.
			// Лежащий транспорт — проблема оператора, видимая в этом логе, а не
			// то, о чём стоит сообщать анонимному вызывающему.
			//
			// Сохранённый код очищается: доставить его уже нельзя, а оставив его
			// на месте, мы держали бы предыдущий код перезаписанным и счётчик
			// попыток обнулённым — и всё зря.
			log.Printf("[PASSWORD RESET] user %s: the code was NOT delivered: %v", user.ID, err)
			if clearErr := s.repo.SetPasswordResetCode(ctx, user.ID, "", time.Now()); clearErr != nil {
				log.Printf("[PASSWORD RESET] user %s: could not clear the undelivered code: %v", user.ID, clearErr)
			}
			return nil
		}
	}

	// Сам код никогда не логируется.
	log.Printf("[PASSWORD RESET] Code sent to user %s (expires %s)", user.ID, expiresAt.Format(time.RFC3339))
	return nil
}

// ResetPassword проверяет код и обновляет пароль.
func (s *AuthService) ResetPassword(ctx context.Context, email, code, newPassword string) error {
	email = strings.TrimSpace(email)
	code = strings.TrimSpace(code)
	if email == "" || code == "" || newPassword == "" {
		return errors.New("укажите Email, код и новый пароль")
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	_, err = s.repo.ResetPasswordWithCode(ctx, email, code, string(hash))
	return err
}

// ChangePassword заменяет пароль вошедшего пользователя после проверки
// текущего и завершает все остальные сессии.
//
// Страница профиля всегда предлагала эту форму; за ней не было эндпоинта,
// поэтому единственным способом сменить пароль был поток восстановления.
func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, oldPassword, newPassword string) (*TokenPair, error) {
	if oldPassword == "" || newPassword == "" {
		return nil, errors.New("укажите текущий и новый пароль")
	}
	if err := validatePassword(newPassword); err != nil {
		return nil, err
	}
	if oldPassword == newPassword {
		return nil, errors.New("новый пароль совпадает с текущим")
	}

	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, errors.New("user not found")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword)); err != nil {
		return nil, errors.New("текущий пароль неверен")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdatePassword(ctx, userID, string(hash)); err != nil {
		return nil, err
	}

	// Все, кто ещё был в системе со старым паролем, выходят. Вызывающий получает
	// свежую пару, чтобы устройство, сделавшее изменение, осталось рабочим.
	if err := s.RevokeAllSessions(ctx, userID); err != nil {
		log.Printf("[AuthService] failed to end sessions after password change for %s: %v", userID, err)
	}
	return s.IssueTokenPair(ctx, user)
}

// UpdateUserEmail обновляет почту пользователя и запускает подтверждение.
func (s *AuthService) UpdateUserEmail(ctx context.Context, userID uuid.UUID, newEmail string) (*repository.User, error) {
	newEmail = strings.TrimSpace(newEmail)
	if newEmail == "" || !emailRegex.MatchString(newEmail) {
		return nil, errors.New("a valid email is required")
	}

	currentUser, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	existingUser, err := s.repo.FindByEmail(ctx, newEmail)
	if err == nil && existingUser != nil && existingUser.ID != userID {
		log.Printf("[SECURITY NOTICE] User with phone %s (ID: %s) attempted to attach email %s which is already bound to user with phone %s (ID: %s)",
			currentUser.Phone, currentUser.ID, newEmail, existingUser.Phone, existingUser.ID)
		return nil, errors.New("что-то пошло не так")
	}

	verificationToken := uuid.New().String()
	tokenExpiresAt := time.Now().Add(60 * time.Minute)
	user, err := s.repo.UpdateUserEmail(ctx, userID, newEmail, verificationToken, tokenExpiresAt)
	if err != nil {
		return nil, err
	}

	if s.mailer != nil {
		if err := s.mailer.SendEmailVerification(newEmail, verificationToken); err != nil {
			log.Printf("[AuthService] Failed to send email verification to %s: %v", newEmail, err)
		} else {
			log.Printf("[AuthService] Successfully triggered email verification to %s", newEmail)
		}
	}

	return user, nil
}

// UpdateUserBirthDate обновляет дату рождения пользователя.
func (s *AuthService) UpdateUserBirthDate(ctx context.Context, userID uuid.UUID, birthDateStr string) (*repository.User, error) {
	t, err := parseBirthDate(birthDateStr)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateUserBirthDate(ctx, userID, t); err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, userID)
}
