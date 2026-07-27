# План исправлений в коде для 3 этапа

## Статус реализации

| Раздел | Статус |
| :--- | :--- |
| Миграции базы данных | ✅ Реализовано (`002_create_tables.sql`, `004_geozones_and_orders.sql`) |
| JWT middleware | ✅ Реализовано (`backend/middleware/auth.go`) |
| Расширение auth (refresh/logout/сброс пароля) | ✅ Реализовано (`backend/service/auth.go`, `backend/handler/admin.go`) |
| Репозитории order/shift/geozone/transaction/bid/chat | ✅ Реализовано |
| Сервисы OrderService, ShiftService, BidService, ChatService, MatchingService | ✅ Реализовано |
| Финансовая логика (холд, выплата, возврат, штраф) | ✅ Реализовано |
| Геозоны и штрафы за 3 нарушения подряд | ✅ Реализовано |
| Хэндлеры customer/executor/admin | ✅ Реализовано |
| Background workers (matching, SLA, auction) | ✅ Реализовано |
| main.go | ✅ Обновлён |
| Unit-тесты | ✅ Добавлены/обновлены |

---

## 1. Миграции базы данных

### 1.1. Создать `backend/migrations/003_add_stage3_tables.sql`

* Таблица `geozones` для хранения рабочих зон исполнителей.
* Таблица `shift_gps_logs` для логирования GPS-координат.
* Таблица `refresh_tokens` для управления refresh-токенами.
* Индексы по внешним ключам.

### 1.2. Обновить `backend/migrations/002_create_tables.sql`

Добавить в таблицу `orders` служебные поля:

* `assigned_at TIMESTAMP WITH TIME ZONE` — время назначения исполнителя;
* `canceled_at TIMESTAMP WITH TIME ZONE` — время отмены;
* `deadline_at TIMESTAMP WITH TIME ZONE` — крайний срок выполнения (для срочных тарифов).

## 2. Middleware авторизации

### 2.1. Создать `backend/middleware/auth.go`

* Функция `JWTVerifier(secret []byte) func(http.Handler) http.Handler`:
  * читает заголовок `Authorization: Bearer <token>`;
  * парсит и валидирует JWT;
  * записывает claims (`user_id`, `phone`, `role`) в `context.Context`.
* Функция `RequireRole(roles ...string) func(http.Handler) http.Handler`:
  * проверяет роль из контекста;
  * возвращает `403 Forbidden`, если роль не подходит.

## 3. Расширение сервиса авторизации

### 3.1. Обновить `backend/service/auth.go`

* Добавить публичный тип `JWTClaims` для типизированного доступа к claims.
* Добавить метод `ParseJWT(token string) (*JWTClaims, error)` для использования в middleware.
* Добавить методы:
  * `IssueRefreshToken(user *User, deviceID string) (string, error)`;
  * `RotateRefreshToken(token string) (string, string, error)`;
  * `RevokeRefreshToken(token string) error`;
  * `ChangePassword(userID uuid.UUID, oldPassword, newPassword string) error`;
  * `ResetPassword(phone, code, newPassword string) error`.

## 4. Репозитории

### 4.1. Создать `backend/repository/order.go`

Интерфейс `OrderRepository`:

* `Create(order *Order) error`;
* `FindByID(id uuid.UUID) (*Order, error)`;
* `FindAvailableByGeozone(geozoneID int) ([]Order, error)`;
* `Assign(orderID, executorID uuid.UUID) error`;
* `Confirm(orderID uuid.UUID, finalAmount float64) error`;
* `Cancel(orderID uuid.UUID) error`;
* `UpdateFinalAmount(orderID uuid.UUID, amount float64) error`.

### 4.2. Создать `backend/repository/shift.go`

Интерфейс `ShiftRepository`:

* `Create(shift *Shift) error`;
* `FindActiveByExecutor(executorID uuid.UUID) (*Shift, error)`;
* `End(shiftID uuid.UUID) error`;
* `Penalize(shiftID uuid.UUID, fine float64) error`;
* `SaveGPSLog(log *GPSLog) error`.

### 4.3. Создать `backend/repository/geozone.go`

Интерфейс `GeozoneRepository`:

* `FindByID(id int) (*Geozone, error)`;
* `FindByExecutor(userID uuid.UUID) (*Geozone, error)`.

### 4.4. Создать `backend/repository/refresh_token.go`

Интерфейс `RefreshTokenRepository`:

* `Create(token *RefreshToken) error`;
* `FindByTokenHash(hash string) (*RefreshToken, error)`;
  * `Revoke(id uuid.UUID) error`;
  * `RevokeAllByUser(userID uuid.UUID) error`.

## 5. Сервисы

### 5.1. Создать `backend/service/order.go`

`OrderService` с методами:

* `Create(customerID uuid.UUID, req CreateOrderRequest) (*Order, error)`;
* `Accept(orderID, executorID uuid.UUID) error`;
* `Confirm(customerID, orderID uuid.UUID) error`;
* `Cancel(customerID, orderID uuid.UUID) error`;
* `ListAssigned(executorID uuid.UUID) ([]Order, error)`.

Бизнес-правила:

* проверка баланса заказчика;
* расчёт `deadline_at` на основе тарифа;
* даунгрейд тарифа «Вынести сейчас» при нарушении SLA;
* возврат переплаты заказчику.

### 5.2. Создать `backend/service/shift.go`

`ShiftService` с методами:

* `Start(executorID uuid.UUID, durationHours int) (*Shift, error)`;
* `End(executorID uuid.UUID) error`;
* `RecordLocation(executorID uuid.UUID, lat, lon float64) error`;
* `CheckGeofence(executorID uuid.UUID, lat, lon float64) (bool, error)`.

Бизнес-правила:

* у исполнителя может быть только одна активная смена;
* проверка попадания в геозону;
* штраф после 3 последовательных нарушений;
* запрет на получение новых заказов при статусе `PENALIZED`.

## 6. Хэндлеры

### 6.1. Создать `backend/handler/order.go`

Эндпоинты:

* `POST /customer/orders`;
* `POST /customer/orders/{id}/cancel`;
* `POST /customer/orders/{id}/confirm`;
* `POST /executor/orders/{id}/accept`;
* `GET /executor/orders/assigned`.

### 6.2. Создать `backend/handler/shift.go`

Эндпоинты:

* `POST /executor/shifts`;
* `POST /executor/shifts/location`;
* `POST /executor/shifts/end`.

### 6.3. Обновить `backend/handler.go`

Добавить эндпоинты авторизации:

* `POST /auth/refresh`;
* `POST /auth/logout`;
* `POST /auth/password/reset`;
* `POST /auth/password/change`.

## 7. Обновление `backend/main.go`

* Подключить `middleware.JWTVerifier` ко всем маршрутам, кроме `/health`, `/register`, `/login`.
* Применить `middleware.RequireRole` к маршрутам `/customer/*` и `/executor/*`.
* Инициализировать новые репозитории, сервисы и хэндлеры.
* Зарегистрировать новые маршруты.

## 8. Тестирование

* Добавить unit-тесты для `OrderService` и `ShiftService` с мок-репозиториями.
* Добавить интеграционные тесты для критичных сценариев:
  * создание заказа с холдированием;
  * принятие заказа исполнителем;
  * подтверждение заказа с выплатой;
  * отмена заказа с возвратом средств;
  * выход исполнителя из геозоны и начисление штрафа.
