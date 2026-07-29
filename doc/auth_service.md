# Сервис авторизации (`AuthService`)

## Назначение

`AuthService` отвечает за:

- регистрацию нового пользователя по номеру телефона, паролю и адресу забора мусора;
- аутентификацию существующего пользователя;
- выпуск подписанного JWT-токена после успешной аутентификации.

Сервис принимает абстракцию `repository.UserRepository`, что позволяет тестировать бизнес-логику отдельно от слоя хранения.

## Расположение в коде

```
backend/
├── service/
│   ├── auth.go       # реализация AuthService
│   └── auth_test.go  # юнит-тесты
├── repository/
│   └── user.go       # контракт UserRepository и реализация на SQL
└── handler/
    └── public.go     # HTTP-обработчики регистрации/входа
```

## Зависимости

```go
type UserRepository interface {
    FindByPhone(phone string) (*User, error)
    Create(user *User) error
    FindByID(id uuid.UUID) (*User, error)
    UpdateStatus(id uuid.UUID, status string) error
    UpdateRole(id uuid.UUID, role string) error
    UpdateBalance(id uuid.UUID, balance float64) error
    UpdateLastGeo(id uuid.UUID, lastGeo string) error
    CreateCustomerProfile(userID uuid.UUID, address string) error
    GetCustomerProfile(userID uuid.UUID) (*CustomerProfile, error)
    UpdateCustomerAddress(userID uuid.UUID, address string) error
}
```

## Создание сервиса

```go
repo := repository.New(db)
authSvc := service.NewAuthService(repo) // читает JWT_SECRET из env
```

## Публичные методы

### Register

```go
func (s *AuthService) Register(phone, password, address string) (*repository.User, error)
```

Создаёт нового пользователя-заказчика.

**Поведение:**
1. Проверяет, что `phone`, `password` и `address` не пустые.
2. Проверяет уникальность телефона.
3. Хеширует пароль `bcrypt`.
4. Создаёт запись в `users` (роль `CUSTOMER`, статус `ACTIVE`).
5. Создаёт профиль в `customer_profiles` с указанным адресом.

**Возвращаемые ошибки:**
- `phone and password are required`
- `address is required`
- `user already exists`

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

## Безопасность

* Пароли хешируются `bcrypt`.
* JWT-подпись через `JWT_SECRET`.
* Ошибки аутентификации не раскрывают существование аккаунта.

## Переменные окружения

| Переменная | Назначение | Значение по умолчанию |
|------------|------------|----------------------|
| `JWT_SECRET` | Секрет для подписи JWT | `dev-secret-change-me` |

## Ограничения текущей реализации

* Нет SMS-верификации номера телефона.
* Нет проверки формата телефона.
* Нет требований к сложности пароля.
* Нет механизма сброса/обновления пароля.
* Нет refresh-токенов.
