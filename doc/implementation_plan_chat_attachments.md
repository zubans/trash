# План реализации: Загрузка файлов и фотографий (Галерея, Камера, Документы) в Чат

Документ содержит план добавления прикрепления фото с камеры, из галереи и файлов в чат для веб-клиента и Android-приложения.

---

## 1. Обзор требований

1. **Мобильное Android-приложение**:
   - Прикрепление фото с **камеры** устройства (`capture="environment"` / нативный выбор).
   - Прикрепление фото из **галереи Android** (выбор изображений `.jpg`, `.png`, `.webp`, `.heic`).
   - Прикрепление любых **файлов и документов** (`.pdf`, `.doc`, `.zip`, `.txt` и т.д.).
2. **Веб-приложение (Десктоп/Мобильный браузер)**:
   - Прикрепление файлов с устройства через стандартное диалоговое окно выбора файлов.
3. **Отображение в чате Telegram-style**:
   - Картинки рендерятся в виде предпросмотра изображения с возможностью клика для полноэкранного просмотра.
   - Документы и файлы рендерятся в виде карточки файла с иконкой скрепки, оригинальным именем, размером файла и кнопкой скачивания.

---

## 2. Предлагаемые изменения

### 2.1. Изменения в БД (`PostgreSQL`)

#### Миграция `backend/migrations/016_add_chat_message_attachments.sql`
- Добавление опциональных колонок в таблицу `messages`:
  ```sql
  ALTER TABLE messages ADD COLUMN IF NOT EXISTS file_url TEXT NULL;
  ALTER TABLE messages ADD COLUMN IF NOT EXISTS file_name TEXT NULL;
  ALTER TABLE messages ADD COLUMN IF NOT EXISTS file_type VARCHAR(50) NULL; -- "image", "document"
  ALTER TABLE messages ADD COLUMN IF NOT EXISTS file_size BIGINT NULL;
  ```

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
      Status    string     `json:"status"`
      FileURL   *string    `json:"file_url,omitempty"`
      FileName  *string    `json:"file_name,omitempty"`
      FileType  *string    `json:"file_type,omitempty"` // "image" или "document"
      FileSize  *int64     `json:"file_size,omitempty"`
      CreatedAt time.Time  `json:"created_at"`
      ReadAt    *time.Time `json:"read_at,omitempty"`
  }
  ```
- Метод `SaveMessageWithAttachment(chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64)` для сохранения сообщения с файлом.

#### `backend/handler/chat.go`
- Новый REST-эндпоинт: **`POST /api/chats/{order_id}/upload`** (`multipart/form-data`).
  - Принимает загруженный файл (`file`) + опциональное текстовое описание (`text`).
  - Сохраняет файл в локальное хранилище `uploads/chat/` под уникальным UUID названием.
  - Сохраняет сообщение в БД с `file_url`, `file_name`, `file_type` (`image` для изображений, `document` для файлов) и `file_size`.
  - Рассылает готовое `Message` сообщение всем участникам WebSocket-комнаты.

#### `backend/main.go`
- Монтирование публичного раздатчика статических файлов: `r.Get("/uploads/*", ...)` для доступа к прикрепленным картинкам и файлам.

---

### 2.3. Фронтенд (Vue 3 / TypeScript)

#### UI ЧАТА Telegram-style (`CustomerDashboard.vue` и `ExecutorDashboard.vue`)

1. **Кнопка Скрепка (Attachment Clip)**:
   - В нижнюю панель ввода сообщений рядом с текстовым полем добавляется кругленькая кнопка со скрепкой 📎:
     ```html
     <button type="button" class="telegram-attach-btn" @click="triggerFileInput">
       📎
     </button>
     <input type="file" ref="fileInputRef" style="display: none;" @change="handleFileSelected" />
     <input type="file" ref="cameraInputRef" accept="image/*" capture="environment" style="display: none;" @change="handleFileSelected" />
     ```

2. **Диалог выбора на мобилке (Камера / Галерея / Файл)**:
   - На Android при нажатии на скрепку всплывает нативный/стилизованный выбор action-sheet:
     - 📸 **Камера** (`capture="environment"`)
     - 🖼️ **Галерея / Изображения** (`accept="image/*"`)
     - 📄 **Файлы и Документы** (`accept="*/*"`)

3. **Предпросмотр перед отправкой**:
   - При выборе файла над полем ввода появляется превью плашка с кнопкой отмены `✕` и прогресс-баром загрузки.

4. **Отображение медиа и файлов в пузырях чата**:
   - **Картинки**: Рендерятся прямо в баббле чата с закругленными углами и подписью. При клике открывается полноразмерное изображение.
   - **Документы**: Рендерятся в виде прямоугольной карточки файла с оригинальным именем (например, `contract.pdf`), человекочитаемым размером (например, `2.4 MB`) и ссылкой на скачивание.

---

## 3. План верификации

1. **Сборка проекта**:
   - `cd backend && go build ./...` — проверка бэкенда.
   - `cd frontend && npm run build` — проверка отсутствия ошибок во фронтенде.
2. **Ручная проверка**:
   - Отправка текстового сообщения с прикрепленным фото с Камеры.
   - Отправка файла `.pdf` из Галереи / Памяти устройства.
   - Скачивание прикрепленного файла собеседником.
