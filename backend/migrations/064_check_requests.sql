-- 064_check_requests.sql
-- Заявка на подтверждение статуса «проверенный».
--
-- Паспорт, который заказчик отдал на хранение прямо на верификации, — это и
-- есть его просьба подтвердить данные: модератор уже держит документ в руках и
-- сверил его. Поэтому заявка ставится сама, без отдельного обращения в
-- поддержку, и попадает в очередь модерации.
--
-- Отметка снимается вместе с решением: поставили «проверенный» или отказали —
-- заявки больше нет.

ALTER TABLE users ADD COLUMN IF NOT EXISTS check_requested_at TIMESTAMPTZ NULL;
CREATE INDEX IF NOT EXISTS idx_users_check_requested
    ON users (check_requested_at) WHERE check_requested_at IS NOT NULL;
