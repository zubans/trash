# Сервис авторизации (`AuthService`)

## Назначение

`AuthService` отвечает за:

- регистрацию нового пользователя по номеру телефона, обязательному email, паролю и адресу начала работы/забора мусора;
- верификацию email и процедуру сброса пароля по 6-значному коду;
- аутентификацию существующего пользователя;
- выпуск подписанного JWT-токена после успешной аутентификации.

Сервис принимает абстракцию `repository.UserRepository`, что позволяет тестировать бизнес-логику отдельно от слоя хранения.

## Расположение в коде

```
backend/
├── service/
│   ├── auth.go       # реализация AuthService (регистрация, подтверждение email, сброс пароля)
│   ├── address.go    # модель адреса (части, сборка строки, валидация)
│   ├── dadata.go     # подсказки адресов DaData
│   ├── address_suggest.go # единая точка входа подсказок + legacy-формат
│   ├── geocoder.go   # геокодирование адресов (Nominatim + кэш)
│   └── auth_test.go  # юнит-тесты
├── repository/
│   └── user.go       # контракт UserRepository и реализация на SQL
└── handler/
    └── public.go     # HTTP-обработчики регистрации/входа/сброса пароля
```

## Зависимости

```go
type UserRepository interface {
    FindByPhone(phone string) (*User, error)
    FindByEmail(email string) (*User, error)
    FindByEmailVerificationToken(token string) (*User, error)
    VerifyEmailToken(token string) (*User, error)
    SetPasswordResetCode(userID uuid.UUID, code string, expiresAt time.Time) error
    ResetPasswordWithCode(email, code, newHashedPassword string) (*User, error)
    Create(user *User) error
    FindByID(id uuid.UUID) (*User, error)
    UpdateStatus(id uuid.UUID, status string) error
    UpdateRole(id uuid.UUID, role string) error
    UpdateBalance(id uuid.UUID, balance float64) error
    UpdateLastGeo(id uuid.UUID, lastGeo string) error
    CreateCustomerProfile(userID uuid.UUID, address CustomerAddress, lastGeo string) error
    GetCustomerProfile(userID uuid.UUID) (*CustomerProfile, error)
    UpdateCustomerAddress(userID uuid.UUID, address CustomerAddress) error
}
```

## Создание сервиса

```go
repo := repository.New(db)
geocoder := service.NewGeocoder(db)
authSvc := service.NewAuthService(repo, geocoder) // читает JWT_SECRET из env
```

## Публичные методы

### RegisterWithAddress / RegisterWithCoordinates

```go
func (s *AuthService) RegisterWithAddress(phone, email, password, lastName, firstName, patronymic, address string, structured Address, role string, lat, lon *float64) (*repository.User, error)
func (s *AuthService) RegisterWithCoordinates(phone, email, password, lastName, firstName, patronymic, address, role string, lat, lon *float64) (*repository.User, error)
```

Создаёт нового пользователя (`CUSTOMER` или `EXECUTOR`).

`RegisterWithAddress` — основной метод: принимает адрес уже разложенный на части (так его присылает клиент, собранный против списка подсказок). `RegisterWithCoordinates` оставлен как обёртка для клиентов, которые шлют адрес одной строкой.

**Поведение:**
1. Проверяет, что `phone`, `email`, `password` и `address` заполнены.
2. Валидирует синтаксис email по регулярному выражению `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`.
3. Проверяет уникальность телефона и email.
4. Хеширует пароль `bcrypt`.
5. Генерирует UUID токен верификации почты `email_verification_token` со сроком действия **60 минут** (`email_token_expires_at`). Введённая почта сохраняется во временном поле `pending_email`, а основное поле `email` остаётся пустым до перехода по ссылке.
6. На указанный `pending_email` отправляется одноразовая ссылка подтверждения. Поле `email` активируется и сохраняется только после успешного перехода пользователя по ссылке.
7. Адрес проверяется по содержанию, а не по написанию: нужны населённый пункт, улица и дом (`Address.Validate`). Если части не пришли, строка раскладывается на сервере (`ParseAddressLine`). Части сохраняются рядом со строкой — см. [`address_suggestions.md`](./address_suggestions.md).

> Прежняя проверка требовала формат `Россия, Город, Улица, д.<цифры> [кв. <цифры>]` и, как следствие, чисто числовой номер дома — человек с адресом `12к1` не мог зарегистрироваться вовсе. Регулярное выражение `normalizeAddress` удалено.

### VerifyEmail / RequestPasswordReset / ResetPassword

```go
func (s *AuthService) VerifyEmail(token string) (*repository.User, error)
func (s *AuthService) RequestPasswordReset(email string) (string, error)
func (s *AuthService) ResetPassword(email, code, newPassword string) (*repository.User, error)
```

- `VerifyEmail`: проверяет токен (активен 60 минут), переносит `pending_email` в поле `email`, очищает `pending_email` и устанавливает `email_verified = true`. В противном случае почта не сохраняется.
- `RequestPasswordReset`: генерирует 6-значный цифровой код восстановления (срок действия 30 минут).
- `ResetPassword`: проверяет код и устанавливает новый хеш пароля.

### Authenticate

```go
func (s *AuthService) Authenticate(phone, password string) (*repository.User, error)
```

Проверяет учётные данные. Возвращает единый `invalid credentials` при любой проблеме.

### GenerateJWT

```go
func (s *AuthService) GenerateJWT(user *repository.User) (string, error)
```

Создаёт JWT (`HS256`) с claims:
- `sub` — UUID пользователя;
- `phone` — номер телефона;
- `role` — роль;
- `exp` — `now + 15 минут`.

## Модуль отправки почты (`SmtpMailSender`)

В бэкенде реализована служба [`service.SmtpMailSender`](file:///Users/sergeyzyuban/projects/trash/backend/service/mailer.go), интегрированная с встроенным **Maddy Mail Server** (`system@moya-usluga.ru`):

* **`SendEmailVerification(toEmail, token)`**: генерирует HTML-письмо с кнопкой подтверждения аккаунта или смены почты (`/api/auth/verify-email?token=...`).
* **`SendPasswordResetCode(toEmail, code)`**: генерирует HTML-письмо с одноразовым 6-значным цифровым кодом безопасности.
* **`SendBroadcastEmail(req)`**: производит массовую рассылку рекламных и информационных писем по выборочным сегментам (Заказчики, Исполнители или произвольный список Email клиентов).

## Рассылка по учётным записям (`EmailBroadcasts.vue`)

В панели администратора добавлена страница **Рассылки** (`/admin/broadcasts`), позволяющая:
1. Выбирать аудиторию для рассылки:
   - **Заказчики (`CUSTOMERS`)**;
   - **Исполнители (`EXECUTORS`)**;
   - **Рекламные / Произвольные клиенты (`CUSTOM_EMAILS`)** — ввод списка адресов через запятую или с новой строки.
2. Вводить тему и HTML-содержимое письма.
3. Получать подробный отчет о количестве успешно доставленных писем и возникающих ошибках.

## Управление Профилями и Email (Клиентская часть)

1. **Профиль Заказчика (`CustomerProfileModal.vue`)**:
   - Позволяет изменять имеющиеся адреса доставки и привязанную почту Email.
   - При смене Email отправляется `POST /api/user/email`, требующий повторной верификации.
2. **Профиль Исполнителя (`ExecutorProfileModal.vue`)**:
   - Отображает ФИО, телефон, Email и **базовый адрес начала поиска заказов**.
   - Позволяет редактировать базовый адрес (автокомплит) и электронную почту. Экран исполнителя пока использует legacy-эндпоинт `GET /geo/autocomplete` и строковый адрес.

## Безопасность

* Пароли хешируются `bcrypt`.
* JWT-подпись через `JWT_SECRET`.
* Ошибки аутентификации не раскрывают существование аккаунта.
* Обязательная проверка формата Email и верификация токеном.
* **Проверка уникальности Email**: При попытке привязки или смены Email сервис проверяет, не привязан ли адрес к другому пользователю. Если Email найден у другого аккаунта, пользователю возвращается абстрактная ошибка `что-то пошло не так`, а в системный лог фиксируется сопоставление ID и телефонов обособленных пользователей (`[SECURITY NOTICE]`).

## Переменные окружения

| Переменная | Назначение | Значение по умолчанию |
|------------|------------|----------------------|
| `JWT_SECRET` | Секрет для подписи JWT | `dev-secret-change-me` |
