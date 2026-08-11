# План реализации: Оповещения чата, желтые индикаторы непрочитанных сообщений и 3 статуса доставки (Отправлено / Доставлено / Прочитано)

Документ содержит план добавления всплывающих уведомлений, индикаторов непрочитанных сообщений по заказам и 3-этапного отслеживания статусов доставки (Sent / Delivered / Read).

---

## 1. Требования

1. **Всплывающее оповещение сверху**:
   - При поступлении нового сообщения от собеседника в верхней части экрана (в `CustomerDashboard.vue` или `ExecutorDashboard.vue`) появляется всплывающий тост/информер: *"Новое сообщение по заказу #XXXX: [текст]"*.
2. **Желтый индикатор (точечка)**:
   - В списке заказов на карточке/кнопке соответствующего заказа появляется **желтый индикатор (точка)**, пока у данного заказа есть непрочитанные сообщения и чат с ним не открыт.
3. **3 статуса сообщений для отправителя**:
   - **Отправлено (Sent)** (`status = 'sent'`): Сообщение успешно передано на сервер бэкенда и сохранено в базе данных. (Иконка: `✓` одна серая галочка).
   - **Доставлено (Delivered)** (`status = 'delivered'`): Сообщение доставлено до устройства получателя (получатель в сети, сработало push/websocket событие delivery или поллинг, всплыло сверху уведомление). (Иконка: `✓✓` две серые галочки).
   - **Прочитано (Read)** (`status = 'read'`): Получатель открыл окно чата с этим сообщением (или оно было открыто в момент прихода). (Иконка: `✓✓` две синие галочки).

---

## 2. Предлагаемые изменения

### 2.1. Изменения в БД (`PostgreSQL`)

#### Миграция `backend/migrations/015_add_message_status.sql`
- Добавление колонки `status VARCHAR(20) NOT NULL DEFAULT 'sent'` в таблицу `messages`.
- Добавление колонки `read_at TIMESTAMPTZ NULL` в таблицу `messages`.
- Индексы для быстрого подсчета непрочитанных сообщений: `CREATE INDEX idx_messages_chat_status ON messages(chat_id, status)`.

---

### 2.2. Бэкенд (Go)

#### `backend/repository/chat.go`
- Обновление структуры `Message`:
  ```go
  type Message struct {
      ID        uuid.UUID  `json:"id"`
      ChatID    uuid.UUID  `json:"chat_id"`
      SenderID  uuid.UUID  `json:"sender_id"`
      Text      string     `json:"text"`
      Status    string     `json:"status"` // "sent", "delivered", "read"
      CreatedAt time.Time  `json:"created_at"`
  }
  ```
- Метод `MarkMessagesAsDelivered(chatID, recipientID uuid.UUID) ([]uuid.UUID, error)` — переводит входящие сообщения пользователя в статус `delivered`.
- Метод `MarkMessagesAsRead(chatID, recipientID uuid.UUID) ([]uuid.UUID, error)` — переводит входящие сообщения пользователя в статус `read`.
- Метод `GetUnreadCounts(userID uuid.UUID) (map[uuid.UUID]int, error)` — получение списка order_id с количеством непрочитанных сообщений для badge точечек.

#### `backend/service/chat.go`
- Обработка новых типов WebSocket-событий:
  - `{"type":"delivery_ack", "order_id":"..."}` — клиент сообщает серверу, что получил сообщение (статус -> `delivered`). Бэкенд рассылает отправителю `{"type":"status_update", "message_ids":[...], "status":"delivered"}`.
  - `{"type":"read_ack", "order_id":"..."}` — клиент открыл чат заказа (статус -> `read`). Бэкенд рассылает отправителю `{"type":"status_update", "message_ids":[...], "status":"read"}`.

#### `backend/handler/chat.go`
- Новый REST handler `POST /api/chats/{order_id}/read` для отметки прочтения при REST fallback.
- Новый REST handler `GET /api/chats/unread-summary` для первоначальной загрузки списка непрочитанных заказов.

---

### 2.3. Фронтенд (Vue 3 / TypeScript)

#### `frontend/src/pages/customer/CustomerDashboard.vue` и `frontend/src/pages/executor/ExecutorDashboard.vue`

1. **Верхний всплывающий Toast (Notification)**:
   - Добавление всплывающей плашки `chatNotification` в верхней части экрана:
     ```html
     <div v-if="chatToast" class="chat-top-toast animate-slide-down" @click="openChatByOrder(chatToast.order)">
       <div class="toast-icon">💬</div>
       <div class="toast-body">
         <div class="toast-title">{{ chatToast.title }}</div>
         <div class="toast-text">{{ chatToast.text }}</div>
       </div>
     </div>
     ```
   - Плашка появляется при получении `onmessage`, если чат данного заказа сейчас **не открыт**.
   - При получении фонового сообщения отправляется событие `delivery_ack` (статус становится **Доставлено**).

2. **Желтый индикатор (Yellow Badge Dot)**:
   - Хранение массива/сет `unreadOrders = ref(new Set<string>())`.
   - В списке карточек заказов на кнопке «Чат» рендерится стильный желтый кружок-индикатор:
     ```html
     <va-button @click="openChat(order)" class="position-relative">
       {{ $t('common.chat') }}
       <span v-if="unreadOrders.has(order.id)" class="yellow-unread-dot"></span>
     </va-button>
     ```
   - При открытии чата с заказом высылается `read_ack` (или `POST /api/chats/{id}/read`), индикатор удаляется.

3. **3 Статуса отображения галочек сообщения**:
   - Обновление компонента визуализации галочек сообщений:
     - `status === 'sent'` — `✓` (одна серая галочка).
     - `status === 'delivered'` — `✓✓` (две серые галочки).
     - `status === 'read'` — `✓✓` (две синие яркие галочки Telegram-style `#5bb3f0`).

---

## 3. План верификации

### Автоматическое и ручное тестирование:
1. **Проверка компиляции бэкенда и фронтенда**:
   - `cd frontend && npm run build` — проверка отсутствия ошибок TypeScript/Vue.
2. **Ручная проверка пользовательских сценариев**:
   - **Сценарий 1 (Отправлено -> Доставлено)**: Пользователь A пишет сообщение пользователю B. Сообщение первоначально имеет 1 серую галочку `✓`. Когда устройство B принимает WS-фрейм или поллинг, у A иконка переключается на 2 серые галочки `✓✓`.
   - **Сценарий 2 (Верхний Toast & Желтая точка)**: Если пользователь B находится на другой странице/карточке и чат закрыт, вверху экрана всплывает плашка с сообщением, а на карточке заказа появляется желтый кружок.
   - **Сценарий 3 (Прочитано)**: Пользователь B открывает чат. Желтая точка гаснет. У пользователя A 2 серые галочки моментально сменяются на 2 синие галочки `✓✓`.
