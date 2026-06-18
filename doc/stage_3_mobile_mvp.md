# Этап 3: Базовые мобильные приложения (Android MVP) — Спецификация реализации

В данном документе приведено детальное описание архитектуры, структуры файлов, API эндпоинтов и алгоритмов для запуска базовых мобильных приложений (Android) и поддержки функционала заказов с геофиксацией.

---

## 1. Архитектура и расположение файлов

Для разработки Android-приложений используется нативный стек: Kotlin, Jetpack Compose (для UI), Retrofit (для REST API) и WorkManager (для фонового сбора данных).

### Дополнения в структуре бэкенда (`backend/`)
```
backend/
├── handler/
│   ├── order.go                # [NEW] Обработка эндпоинтов заказов (создание, подтверждение)
│   └── shift.go                # [NEW] Управление сменами и координатами исполнителей
├── service/
│   ├── order.go                # [NEW] Бизнес-логика заказов и холдирования средств
│   └── shift.go                # [NEW] Бизнес-логика смен и проверки геопозиции
└── repository/
    ├── order.go                # [NEW] SQL-запросы для таблиц orders и transactions
    └── shift.go                # [NEW] SQL-запросы для смен и записи координат
```

### Структура мобильного приложения (`android/`)
```
android/app/src/main/java/com/trash/app/
├── data/
│   ├── api/
│   │   ├── ApiClient.kt        # Инстанс Retrofit с JWT-авторизацией
│   │   └── TrashService.kt     # Интерфейсы API (регистрация, заказы, смены)
│   ├── repository/
│   │   ├── OrderRepository.kt  # Управление заказами
│   │   └── ShiftRepository.kt  # Управление сменами и координатами
│   └── model/
│       └── DomainModels.kt     # Модели данных (User, Order, Shift, Location)
├── service/
│   └── LocationTrackingService.kt # [NEW] Фоновый сервис для записи и отправки GPS-координат
├── worker/
│   └── LocationUploadWorker.kt # [NEW] Задача WorkManager для периодического сбора геоданных
└── ui/
    ├── auth/
    │   └── AuthScreen.kt       # Общий экран входа / регистрации
    ├── customer/
    │   ├── CustomerDashboard.kt # Главный экран заказчика (баланс, список заказов)
    │   └── OrderCreateScreen.kt # Экран создания нового заказа
    └── executor/
        ├── ExecutorDashboard.kt # Главный экран исполнителя (карты, смены)
        └── ShiftActiveScreen.kt # Экран активной смены и навигации по заказам
```

---

## 2. Спецификация API (Новые эндпоинты)

| Метод | Эндпоинт | Описание | Роль |
| :--- | :--- | :--- | :--- |
| **POST** | `/customer/orders` | Создать заказ (стандартный тариф, hold средств) | `CUSTOMER` |
| **POST** | `/customer/orders/{id}/confirm` | Подтвердить забор мусора (выплата вознаграждения) | `CUSTOMER` |
| **POST** | `/executor/shifts` | Выйти на рабочую смену (1, 3 или 5 часов) | `EXECUTOR` |
| **POST** | `/executor/shifts/location` | Отправка текущих координат исполнителя из фонового сервиса | `EXECUTOR` |
| **GET** | `/executor/orders/assigned` | Получить список текущих активных заказов | `EXECUTOR` |

---

## 3. Слой данных и SQL-запросы

Для контроля смен и геопозиционирования добавляется таблица логирования координат.

### Схема таблицы GPS-логов
```sql
CREATE TABLE IF NOT EXISTS shift_gps_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    shift_id UUID NOT NULL REFERENCES shifts(id) ON DELETE CASCADE,
    latitude NUMERIC(9,6) NOT NULL,
    longitude NUMERIC(9,6) NOT NULL,
    recorded_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

-- Индекс по shift_id для быстрого построения трека
CREATE INDEX idx_gps_logs_shift ON shift_gps_logs(shift_id);
```

### Основные SQL-запросы

1. **Создание заказа с холдированием средств (`CreateOrderWithHold`)**:
   Выполняется в рамках единой базы транзакции (`Tx`).
   ```sql
   -- Шаг 1: Блокировка и получение баланса заказчика
   SELECT balance FROM users WHERE id = $1 FOR UPDATE;

   -- Шаг 2: Проверка баланса. Если balance >= amount, списываем баланс
   UPDATE users SET balance = balance - $2 WHERE id = $1;

   -- Шаг 3: Создание заказа со статусом SEARCHING и фиксацией hold_amount
   INSERT INTO orders (customer_id, volume_type, speed_tariff, hold_amount, final_amount, status)
   VALUES ($1, $3, $4, $2, $2, 'SEARCHING') RETURNING id;

   -- Шаг 4: Логирование транзакции блокировки
   INSERT INTO transactions (user_id, order_id, type, amount)
   VALUES ($1, $5, 'HOLD', $2);
   ```

2. **Завершение заказа заказчиком (`ConfirmOrderExecution`)**:
   Выполняется в рамках транзакции (`Tx`).
   ```sql
   -- Шаг 1: Блокировка заказа и проверка статуса (должен быть ASSIGNED или в процессе)
   SELECT hold_amount, executor_id, status FROM orders WHERE id = $1 FOR UPDATE;

   -- Шаг 2: Перевод статуса заказа в COMPLETED
   UPDATE orders SET status = 'COMPLETED', completed_at = now() WHERE id = $1;

   -- Шаг 3: Начисление вознаграждения исполнителю
   UPDATE users SET balance = balance + $2 WHERE id = $3;

   -- Шаг 4: Логирование финансовой выплаты
   INSERT INTO transactions (user_id, order_id, type, amount)
   VALUES ($3, $1, 'REWARD', $2);
   ```

---

## 4. Алгоритмы и логика работы

### А. Алгоритм геофиксации исполнителя и проверки геозоны
Во время смены бэкенд проверяет, находится ли исполнитель в рамках своей рабочей зоны.
Рабочая зона на бэкенде может быть задана в двух форматах:
1. **Круговая зона** (Центр `(center_lat, center_lon)` и радиус `R` в метрах).
2. **Полигональная зона** (Набор точек, описывающих границы района).

#### Для круговой зоны (Формула Гаверсинуса — Haversine Formula)
Используется для вычисления расстояния между двумя точками на сфере Земли:

$$d = 2r \arcsin\left(\sqrt{\sin^2\left(\frac{\Delta \varphi}{2}\right) + \cos(\varphi_1)\cos(\varphi_2)\sin^2\left(\frac{\Delta \lambda}{2}\right)}\right)$$

Где $r$ — средний радиус Земли (6371000 метров), $\varphi$ — широта, $\lambda$ — долгота в радианах.

```go
// Псевдокод на Go для проверки расстояния
func IsWithinRadius(lat1, lon1, lat2, lon2, radius float64) bool {
    const EarthRadius = 6371000.0 // в метрах
    
    dLat := (lat2 - lat1) * math.Pi / 180.0
    dLon := (lon2 - lon1) * math.Pi / 180.0
    
    lat1Rad := lat1 * math.Pi / 180.0
    lat2Rad := lat2 * math.Pi / 180.0
    
    a := math.Sin(dLat/2)*math.Sin(dLat/2) +
        math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(lat1Rad)*math.Cos(lat2Rad)
    c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
    
    distance := EarthRadius * c
    return distance <= radius
}
```

#### Для полигональной зоны (Алгоритм трассировки лучей — Ray-Casting Algorithm)
Если зона задана полигоном, бэкенд проверяет попадание точки `(lat, lon)` внутрь многоугольника. Алгоритм испускает луч из проверяемой точки в бесконечность и считает количество пересечений с ребрами многоугольника: нечетное количество означает нахождение внутри, четное — снаружи.

```go
type Point struct {
    Lat, Lon float64
}

func IsPointInPolygon(p Point, polygon []Point) bool {
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

Если координата исполнителя выходит за пределы зоны в течение 3 последовательных проверок (для исключения ошибок GPS-скачков), статус смены меняется на `PENALIZED` и выставляется штраф (`FINE`).

### Б. Фоновый трекинг на Android
Для обеспечения стабильной отправки координат на Android-клиенте реализуется фоновый сервис `LocationTrackingService` с запуском в режиме `Foreground Service`.
* Сервис отображает постоянное уведомление (Notification) для предотвращения его уничтожения системой Android.
* Используется библиотека **Fused Location Provider Client** для точного и энергоэффективного получения GPS-координат.
* Частота опроса: каждые 5 минут в нормальном режиме, при наличии активного заказа — каждую минуту.
* При потере сети координаты буферизируются во внутреннюю базу данных Room и отправляются пачкой при восстановлении интернет-соединения.
