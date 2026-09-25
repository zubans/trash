-- 066_users_email_lower_unique_and_unread_index.sql
--
-- Почта: уникальность и поиск без учёта регистра.
--
-- FindByEmail искал по LOWER(email), а индекс был по email: каждый вход по
-- почте, регистрация и сброс пароля читали всю таблицу. Уникальность при этом
-- была регистрозависимой — 'A@x.ru' и 'a@x.ru' могли стать двумя учётками.
-- Теперь адреса хранятся в нижнем регистре (сервис приводит на записи), а
-- уникальный индекс построен по LOWER(email) — по нему же ищут FindByEmail и
-- ResetPasswordWithCode. Предикат email <> '' повторяется в их WHERE: без него
-- планировщик не вправе взять частичный индекс.
--
-- Если в базе уже есть адреса, различающиеся только регистром, миграция
-- упадёт на построении индекса — это осознанно: такие дубли надо разобрать
-- руками, а не молча оставить двум людям один адрес.
DROP INDEX IF EXISTS idx_users_email;

UPDATE users SET email = LOWER(email)
WHERE email IS NOT NULL AND email <> LOWER(email);

UPDATE users SET pending_email = LOWER(pending_email)
WHERE pending_email IS NOT NULL AND pending_email <> LOWER(pending_email);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_lower
    ON users (LOWER(email)) WHERE email <> '';

-- Чат: непрочитанные сообщения.
--
-- GetUnreadOrderIDs опрашивается клиентом постоянно и проверяет через EXISTS,
-- есть ли в чате заказа сообщение со status <> 'read'. Индекс (chat_id, status)
-- отвечает на это, перебирая все записи чата; частичный индекс держит только
-- непрочитанные — их мало, и проверка заканчивается на первой же записи.
CREATE INDEX IF NOT EXISTS idx_messages_chat_unread
    ON messages (chat_id) WHERE status <> 'read';
