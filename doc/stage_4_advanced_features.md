# Этап 4: Расширенные функции, Экономика и Связь — Спецификация реализации

В данном документе приведено детальное описание архитектуры, структуры файлов, API эндпоинтов и алгоритмов работы продвинутого функционала платформы (ускоренные тарифы, SLA, торги за строительный мусор и встроенный чат).

---

## 1. Архитектура и расположение файлов

Для реализации чатов реального времени используется протокол WebSockets (на базе пакета `gorilla/websocket` на бэкенде). Фоновые проверки SLA и сроков аукционов выполняются асинхронными воркерами (Workers/Daemons).

### Дополнения в структуре бэкенда (`backend/`)
```
backend/
├── handler/
│   ├── bid.go                  # [NEW] Хендлеры аукциона (ставки, встречные предложения)
│   └── chat.go                 # [NEW] WebSockets и HTTP обработчики сообщений
├── service/
│   ├── bid.go                  # [NEW] Логика торгов и согласования цен
│   └── chat.go                 # [NEW] Менеджер WebSocket-сессий и комнат чатов
├── repository/
│   ├── bid.go                  # [NEW] SQL для взаимодействия с таблицей bids
│   └── chat.go                 # [NEW] SQL для сохранения сообщений и статусов чатов
└── worker/
    ├── sla_worker.go           # [NEW] Фоновый воркер контроля времени прибытия (SLA)
    └── auction_worker.go       # [NEW] Фоновый воркер отслеживания срока жизни аукционов (7 дней)
```

---

## 2. Спецификация API (Дополнительные эндпоинты)

| Метод | Эндпоинт | Описание | Роль |
| :--- | :--- | :--- | :--- |
| **POST** | `/customer/orders/construction` | Создать заявку на строительный мусор (требуется фото) | `CUSTOMER` |
| **POST** | `/executor/orders/{id}/bids` | Сделать ставку / предложить цену за заказ | `EXECUTOR` |
| **POST** | `/customer/bids/{id}/accept` | Принять ставку исполнителя (завершает торги) | `CUSTOMER` |
| **GET** | `/chats/{order_id}/messages` | Получить историю переписки по заказу | `CUSTOMER`/`EXECUTOR` |
| **GET** | `/chats/{order_id}/ws` | WebSocket-соединение для отправки/получения сообщений | `CUSTOMER`/`EXECUTOR` |

---

## 3. Алгоритмы и логика работы

### А. Алгоритм автоматического даунгрейда тарифа (SLA Control Worker)
Воркер `sla_worker.go` запускается по таймеру (например, каждые 30 секунд) в фоновом режиме Go-рутины.

#### Логика работы воркера:
1. Выполняет выборку просроченных заказов с тарифом `ASAP` (15 минут) или `URGENT` (1 час), у которых исполнитель назначен, но не перевел заказ в статус выполнения («Прибыл»), и флаг `is_downgraded` равен `false`:
   ```sql
   SELECT id, customer_id, hold_amount, final_amount, speed_tariff 
   FROM orders 
   WHERE status = 'ASSIGNED' 
     AND is_downgraded = FALSE 
     AND deadline_at < now() 
     AND speed_tariff IN ('ASAP', 'URGENT');
   ```
2. Для каждого просроченного заказа открывается транзакция базы данных (`BEGIN`):
   * Считывается базовая (стандартная) стоимость заказа из `system_settings` для текущего объема мусора.
   * Рассчитывается сумма возврата клиенту: 
     $$\text{Refund} = \text{HoldAmount} - \text{BasePrice}$$
   * Выполняется списание/обновление суммы холдирования в заказе:
     ```sql
     UPDATE orders 
     SET speed_tariff = 'REGULAR', 
         final_amount = $2, 
         is_downgraded = TRUE 
     WHERE id = $1;
     ```
   * Возвращается разница на баланс Заказчика:
     ```sql
     UPDATE users 
     SET balance = balance + $2 
     SET status = 'ACTIVE' 
     WHERE id = $1;
     ```
   * Регистрируется транзакция возврата:
     ```sql
     INSERT INTO transactions (user_id, order_id, type, amount) 
     VALUES ($1, $2, 'REFUND', $3);
     ```
   * Транзакция закрывается (`COMMIT`).
3. Бэкенд отправляет уведомление по WebSocket в приложения Заказчика и Исполнителя об изменении тарифа из-за опоздания.

---

### Б. Алгоритм завершения и отмены торгов (Auction Lifespan Worker)
Воркер `auction_worker.go` ежечасно сканирует базу на предмет устаревших строительных заявок.

```go
// Псевдокод на Go для отмены заявок старше 7 дней
func CheckExpiredAuctions(db *sql.DB) {
    // Выборка заказов CONSTRUCTION, созданных более 7 дней назад и не назначенных исполнителю
    rows, err := db.Query(`
        SELECT id, customer_id, hold_amount 
        FROM orders 
        WHERE status = 'SEARCHING' 
          AND volume_type = 'CONSTRUCTION' 
          AND created_at < now() - INTERVAL '7 days'
    `)
    if err != nil {
        log.Printf("Error selecting expired auctions: %v", err)
        return
    }
    defer rows.Close()

    for rows.Next() {
        var orderID uuid.UUID
        var customerID uuid.UUID
        var holdAmount float64

        // Запуск транзакции по отмене
        tx, _ := db.Begin()
        
        // Перевод заказа в статус CANCELED
        tx.Exec("UPDATE orders SET status = 'CANCELED' WHERE id = $1", orderID)
        
        // Разблокировка заблокированных под торги средств (если были списаны)
        if holdAmount > 0 {
            tx.Exec("UPDATE users SET balance = balance + $1 WHERE id = $2", holdAmount, customerID)
            tx.Exec("INSERT INTO transactions (user_id, order_id, type, amount) VALUES ($1, $2, 'REFUND', $3)", customerID, orderID, holdAmount)
        }
        
        tx.Commit()
        // Отправка push-уведомления заказчику об автоотмене
    }
}
```

---

### В. Механизм контекстной блокировки чата (WebSocket Chat Validation)
Для минимизации нагрузки на бэкенд и обеспечения приватности, WebSocket-хендлер чата выполняет строгие проверки при подключении и при отправке сообщений.

```
[Client] ---> WS Handshake (/chats/{order_id}/ws) 
               |
               v
     [Auth Middleware] (Validate JWT, Extract UserID & Role)
               |
               v
     [Chat Verification]
     1. Проверить, принадлежит ли UserID данному заказу (как Customer или Executor)?
        -> Если нет, закрыть соединение (403 Forbidden).
     2. Проверить статус чата:
        SELECT is_active FROM chats WHERE order_id = $1;
        -> Если is_active = FALSE, перевести соединение в режим "Read-Only".
               |
               +---> [Connection Accepted]
```

#### Логика обработки сообщений (внутри WebSocket-цикла):
* При получении текстового фрейма от клиента, сервер повторно считывает состояние чата из оперативной памяти или БД.
* Если статус заказа в БД перешел в `COMPLETED` или `CANCELED`, сервер принудительно:
  1. Меняет `is_active = FALSE` в таблице `chats`.
  2. Отправляет клиенту системное сообщение `{ "type": "system", "action": "lock" }`.
  3. Закрывает соединение на запись для обоих участников, сохраняя возможность чтения истории по HTTP GET эндпоинту `/chats/{order_id}/messages`.
* Каждое сообщение сохраняется в БД асинхронно через пул воркеров для минимизации задержек доставки в веб-сокет.
