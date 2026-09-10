-- Переписка во внутренней почте.
--
-- До этой миграции ящик был односторонним: ядро клало в него письмо о выданной
-- ачивке, админ рассылал новость, и на этом всё заканчивалось — ответить было
-- некому и нечем. Здесь появляется адресное письмо администратора одному
-- человеку и ответ на него, то есть переписка.
--
-- Переписка живёт в том же user_mail, а не в отдельной таблице: письмо ответа —
-- это письмо, оно лежит в ящике получателя, показывается в той же ленте и
-- удаляется тем же мягким удалением. Разделять их значило бы дважды написать
-- список, чтение, счётчик непрочитанного и удаление.

-- 1. Вид письма: адресное письмо от администрации.
ALTER TABLE user_mail DROP CONSTRAINT IF EXISTS user_mail_kind_check;
ALTER TABLE user_mail ADD CONSTRAINT user_mail_kind_check
    CHECK (kind IN ('ACHIEVEMENT', 'GIFT', 'PROMO', 'NEWS', 'SYSTEM', 'DIRECT'));

-- 2. Направление. Строка всегда лежит в ящике пользователя — и когда письмо
-- пришло ему (IN), и когда он сам ответил администрации (OUT). Так ответ виден
-- обеим сторонам: отправитель видит свою реплику в ленте, а не отправляет её в
-- пустоту.
ALTER TABLE user_mail ADD COLUMN IF NOT EXISTS direction VARCHAR(3) NOT NULL DEFAULT 'IN'
    CHECK (direction IN ('IN', 'OUT'));

-- 3. Ветка переписки: id первого письма. У самого первого письма ссылается на
-- него же — так корень ветки находится одним условием thread_id = id, без
-- отдельного признака. У писем ядра и рассылок NULL: у них ветки нет.
ALTER TABLE user_mail ADD COLUMN IF NOT EXISTS thread_id UUID NULL
    REFERENCES user_mail(id) ON DELETE CASCADE;

-- 4. Когда администрация прочитала ответ. Отдельная колонка, а не read_at:
-- read_at принадлежит владельцу ящика, и отметка админа не должна гасить
-- значок у пользователя.
ALTER TABLE user_mail ADD COLUMN IF NOT EXISTS admin_read_at TIMESTAMPTZ NULL;

-- Лента ветки: письма одной переписки по времени.
CREATE INDEX IF NOT EXISTS idx_user_mail_thread
    ON user_mail (thread_id, created_at) WHERE thread_id IS NOT NULL;

-- Входящие администрации: ответы, которых никто не читал. Частичный индекс —
-- потому что именно эти строки и нужны, а их всегда единицы на фоне ящиков.
-- Условия на deleted_at здесь нет намеренно: мягкое удаление убирает переписку
-- из ящика её владельца, а не из истории обращений, и администратор видит её
-- и после того, как получатель смахнул карточку.
CREATE INDEX IF NOT EXISTS idx_user_mail_admin_unread
    ON user_mail (created_at DESC)
    WHERE direction = 'OUT' AND admin_read_at IS NULL;

-- Список переписок: все адресные письма в порядке свежести.
CREATE INDEX IF NOT EXISTS idx_user_mail_direct
    ON user_mail (user_id, created_at DESC) WHERE kind = 'DIRECT';
