# Этап 3: Базовые мобильные приложения (Android MVP) — Спецификация реализации

В данном документе приведено детальное описание архитектуры, структуры файлов, API эндпоинтов и алгоритмов для запуска базовых мобильных приложений (Android) и поддержки функционала заказов с геофиксацией.

---

## 1. Архитектура и расположение файлов

### Дополнения в структуре бэкенда (`backend/`)

```
backend/
├── middleware/
│   └── auth.go                 # [NEW] Проверка JWT и роли
├── handler/
│   ├── order.go                # [NEW] Обработка эндпоинтов заказов
│   └── shift.go                # [NEW] Управление сменами и координатами
├── service/
│   ├── auth.go                 # существующий сервис авторизации
│   ├── order.go                # [NEW] Бизнес-логика заказов и холдирования средств
│   └── shift.go                # [NEW] Бизнес-логика смен и проверки геопозиции
└── repository/
    ├── user.go                 # существующий репозиторий пользователей
    ├── order.go                # [NEW] SQL-запросы для таблиц orders и transactions
    ├── shift.go                # [NEW] SQL-запросы для смен и записи координат
    └── geozone.go              # [NEW] SQL-запросы для геозон
```

### Структура мобильного приложения (`android/`)

```
android/app/src/main/java/com/trash/app/
├── data/
│   ├── api/
│   │   ├── ApiClient.kt        # Инстанс Retrofit с JWT-авторизацией
│   │   ├── AuthAuthenticator.kt # [NEW] Обновление access-токена через refresh-токен
│   │   └── TrashService.kt     # Интерфейсы API
│   ├── repository/
│   │   ├── OrderRepository.kt
│   │   └── ShiftRepository.kt
│   └── model/
│       └── DomainModels.kt
├── service/
│   └── LocationTrackingService.kt
└── ui/
    ├── auth/
    │   └── AuthScreen.kt
    ├── customer/
    │   ├── CustomerDashboard.kt
    │   └── OrderCreateScreen.kt
    └── executor/
        ├── ExecutorDashboard.kt
        └── ShiftActiveScreen.kt
```

---

## 2. Спецификация API

### Общие правила

* Все эндпоинты, кроме `/register` и `/login`, требуют валидный JWT access-токен в заголовке `Authorization: Bearer <token>`.
* Доступ к эндпоинтам контролируется ролью пользователя (`CUSTOMER`, `EXECUTOR`, `ADMIN`).
* При истечении access-токена клиент использует refresh-токен для получения новой пары через `POST /auth/refresh`.

### Эндпоинты авторизации (дополнения ко 2 этапу)

| Метод | Эндпоинт | Описание | Роль |
| :--- | :--- | :--- | :--- |
| **POST** | `/auth/refresh` | Обновить access-токен по refresh-токену | Любая |
| **POST** | `/auth/logout` | Отозвать текущий refresh-токен | Любая |
| **POST** | `/auth/password/reset` | Сброс пароля по SMS-коду | Любая |
| **POST** | `/auth/password/change` | Обновление пароля авторизованным пользователем | Любая |

### Эндпоинты заказов

| Метод | Эндпоинт | Описание | Роль |
| :--- | :--- | :--- | :--- |
| **POST** | `/customer/orders` | Создать заказ (стандартный тариф, hold средств) | `CUSTOMER` |
| **POST** | `/customer/orders/{id}/cancel` | Отменить активный заказ с возвратом холда | `CUSTOMER` |
| **POST** | `/customer/orders/{id}/confirm` | Подтвердить забор мусора (выплата вознаграждения) | `CUSTOMER` |
| **POST** | `/executor/orders/{id}/accept` | Принять заказ из очереди | `EXECUTOR` |
| **GET** | `/executor/orders/assigned` | Получить текущие активные заказы исполнителя | `EXECUTOR` |

### Эндпоинты смен и геолокации

| Метод | Эндпоинт | Описание | Роль |
| :--- | :--- | :--- | :--- |
| **POST** | `/executor/shifts` | Выйти на рабочую смену (1, 3 или 5 часов) | `EXECUTOR` |
| **POST** | `/executor/shifts/location` | Отправить текущие координаты | `EXECUTOR` |
| **POST** | `/executor/shifts/end` | Завершить активную смену досрочно | `EXECUTOR` |

### Коды ответов

| Статус | Когда возвращается |
| :--- | :--- |
| `200 OK` | Успешный GET-запрос или операция без тела ответа. |
| `201 Created` | Успешное создание заказа, смены или пользователя. |
| `400 Bad Request` | Невалидные входные данные (отсутствуют поля, неверный формат телефона и т.д.). |
| `401 Unauthorized` | Отсутствует токен, токен просрочен или подпись невалидна. |
| `403 Forbidden` | Токен валиден, но роль пользователя не разрешает доступ к эндпоинту. |
| `409 Conflict` | Пользователь с таким телефоном уже существует, у исполнителя уже есть активная смена и т.д. |
| `422 Unprocessable Entity` | Бизнес-правило не выполнено: недостаточно средств, заказ не в статусе `ASSIGNED`, выход за пределы геозоны и т.д. |
| `500 Internal Server Error` | Внутренняя ошибка сервера. |

---

## 3. Слой данных и SQL-запросы

### Миграция 003_add_stage3_tables.sql

Добавляются таблицы для хранения геозон, логов GPS и refresh-токенов.

```sql
-- Геозоны исполнителей
CREATE TABLE IF NOT EXISTS geozones (
    id SERIAL PRIMARY KEY,
    name VARCHAR NOT NULL,
    zone_type VARCHAR NOT NULL CHECK (zone_type IN ('CIRCLE', 'POLYGON')),
    center_lat NUMERIC(9,6),
    center_lon NUMERIC(9,6),
    radius_meters INT,
    polygon JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

-- Логи GPS-координат во время смен
CREATE TABLE IF NOT EXISTS shift_gps_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    shift_id UUID NOT NULL REFERENCES shifts(id) ON DELETE CASCADE,
    latitude NUMERIC(9,6) NOT NULL,
    longitude NUMERIC(9,6) NOT NULL,
    recorded_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

-- Refresh-токены
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR NOT NULL,
    device_id VARCHAR,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    revoked_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_gps_logs_shift ON shift_gps_logs(shift_id);
CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);
```

### Модель холдирования средств

Баланс заказчика не списывается при создании заказа. Вместо этого:

1. Проверяется, что `users.balance >= hold_amount`.
2. Создаётся заказ со статусом `SEARCHING` и полем `hold_amount`.
3. В `transactions` добавляется запись типа `HOLD`.
4. При подтверждении выполнения:
   - списывается `final_amount` с баланса заказчика;
   - начисляется вознаграждение исполнителю;
   - разница `hold_amount - final_amount` (если есть) возвращается заказчику.
5. При отмене заказа холд полностью возвращается на баланс заказчика.

### SQL: создание заказа с холдированием

```sql
-- В рамках транзакции
SELECT balance FROM users WHERE id = $1 FOR UPDATE;

-- Проверка достаточности средств выполняется в сервисном слое
INSERT INTO orders (customer_id, volume_type, speed_tariff, hold_amount, final_amount, status, deadline_at)
VALUES ($1, $2, $3, $4, $4, 'SEARCHING', $5)
RETURNING id;

INSERT INTO transactions (user_id, order_id, type, amount)
VALUES ($1, $6, 'HOLD', $4);
```

### SQL: принятие заказа исполнителем

```sql
-- В рамках транзакции
SELECT status, executor_id FROM orders WHERE id = $1 FOR UPDATE;

UPDATE orders
SET status = 'ASSIGNED', executor_id = $2, assigned_at = now()
WHERE id = $1 AND status = 'SEARCHING' AND executor_id IS NULL;
```

### SQL: подтверждение выполнения заказа

```sql
-- В рамках транзакции
SELECT hold_amount, final_amount, executor_id, status, customer_id
FROM orders WHERE id = $1 FOR UPDATE;

-- Обновление статуса
UPDATE orders SET status = 'COMPLETED', completed_at = now() WHERE id = $1;

-- Списание с баланса заказчика
UPDATE users SET balance = balance - $2 WHERE id = $3;

-- Начисление исполнителю
UPDATE users SET balance = balance + $4 WHERE id = $5;

-- Возврат переплаты заказчику, если final_amount < hold_amount
UPDATE users SET balance = balance + $6 WHERE id = $3;

-- Логирование транзакций
INSERT INTO transactions (user_id, order_id, type, amount) VALUES ($3, $1, 'PAYMENT', $2);
INSERT INTO transactions (user_id, order_id, type, amount) VALUES ($5, $1, 'REWARD', $4);
```

### SQL: отмена заказа

```sql
-- В рамках транзакции
SELECT status, customer_id, hold_amount FROM orders WHERE id = $1 FOR UPDATE;

UPDATE orders SET status = 'CANCELED', canceled_at = now() WHERE id = $1;

UPDATE users SET balance = balance + $2 WHERE id = $3;

INSERT INTO transactions (user_id, order_id, type, amount)
VALUES ($3, $1, 'REFUND', $2);
```

---

## 4. Алгоритмы и логика работы

### А. Назначение заказов исполнителям

На этапе MVP используется упрощённая модель:

1. Заказчик создаёт заказ со статусом `SEARCHING`.
2. Исполнитель, находящийся на активной смене, видит доступные заказы в своей геозоне.
3. Исполнитель принимает заказ через `POST /executor/orders/{id}/accept`.
4. Заказ переходит в статус `ASSIGNED` и закрепляется за исполнителем.
5. Один исполнитель не может принять более одного активного заказа одновременно (опционально для MVP).

### Б. Алгоритм геофиксации исполнителя

Рабочая зона исполнителя задаётся записью в `geozones` и связана с `executor_profiles.work_area_id`.

#### Круговая зона

Используется формула Гаверсинуса для вычисления расстояния между текущей точкой и центром зоны:

```go
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
    const EarthRadius = 6371000.0
    dLat := (lat2 - lat1) * math.Pi / 180.0
    dLon := (lon2 - lon1) * math.Pi / 180.0
    lat1Rad := lat1 * math.Pi / 180.0
    lat2Rad := lat2 * math.Pi / 180.0
    a := math.Sin(dLat/2)*math.Sin(dLat/2) +
        math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
    c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
    return EarthRadius * c
}
```

#### Полигональная зона

Используется алгоритм трассировки луча (ray-casting):

```go
func isPointInPolygon(p Point, polygon []Point) bool {
    inside := false
    j := len(polygon) - 1
    for i := 0; i < len(polygon); i++ {
        if (polygon[i].Lon > p.Lon) != (polygon[j].Lon > p.Lon) &&
            p.Lat < (polygon[j].Lat-polygon[i].Lat)*(p.Lon-polygon[i].Lon)/(polygon[j].Lon-polygon[i].Lon)+polygon[i].Lat {
            inside = !inside
        }
        j = i
    }
    return inside
}
```

#### Обработка выхода из зоны

* Координаты принимаются каждую минуту при активном заказе и каждые 5 минут в обычном режиме.
* Если исполнитель находится вне зоны в течение 3 последовательных проверок, смена переводится в статус `PENALIZED`.
* Начисляется штраф, размер которого берётся из `system_settings` (ключ `outside_zone_fine`).
* Исполнитель больше не получает новые заказы до завершения текущей смены.

### В. Даунгрейд тарифа при нарушении SLA

Для тарифа «Вынести сейчас» (15 минут) установлен жёсткий SLA:

1. При назначении заказа фиксируется `assigned_at`.
2. Если к моменту подтверждения выполнения прошло более 15 минут, заказ автоматически даунгрейдится до стандартного тарифа.
3. `final_amount` пересчитывается по базовому коэффициенту.
4. Разница между `hold_amount` и `final_amount` возвращается на баланс заказчика.
5. Исполнитель получает вознаграждение по уменьшенному `final_amount`.

### Г. Фоновый трекинг на Android

* Используется foreground service `LocationTrackingService` с постоянным уведомлением.
* Для получения координат применяется **Fused Location Provider Client**.
* Частота обновления:
  * активный заказ — каждую минуту;
  * активная смена без заказа — каждые 5 минут.
* При отсутствии сети координаты сохраняются в локальную базу Room и отправляются пачкой при восстановлении соединения.
* Необходимые разрешения в `AndroidManifest.xml`:
  * `ACCESS_FINE_LOCATION`;
  * `ACCESS_COARSE_LOCATION`;
  * `FOREGROUND_SERVICE`;
  * `FOREGROUND_SERVICE_LOCATION` (Android 14+);
  * `POST_NOTIFICATIONS` (Android 13+).
* Для стабильной работы на современных Android-версиях рекомендуется запрашивать отключение батарейной оптимизации для приложения.

---

## 5. Безопасность и ограничения

* Все эндпоинты stage 3 работают только по HTTPS.
* Доступ к заказам контролируется по `user_id` из JWT: заказчик может управлять только своими заказами, исполнитель — только назначенными ему.
* На критичных операциях (`CreateOrder`, `AcceptOrder`, `ConfirmOrder`) применяется `SELECT ... FOR UPDATE` для предотвращения race condition.
* Частота запросов на отправку координат ограничивается rate limiting.
