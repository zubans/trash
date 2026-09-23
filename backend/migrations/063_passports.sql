-- 063_passports.sql
-- Паспорт, статус «проверенный» и согласие на обработку персональных данных
-- (implementation_plan_delivery_passport.md §2–§4).
--
-- «Проверенный» — отдельный флаг, не статус учётной записи и не верификация:
-- is_verified говорит, что модератор сверил ФИО и дату рождения, is_checked —
-- что в системе есть паспорт с фото и человек его просмотрел. Ставится только
-- руками, в админке.

ALTER TABLE users ADD COLUMN IF NOT EXISTS is_checked BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS checked_at TIMESTAMPTZ NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS checked_by UUID NULL REFERENCES users(id);

-- Согласие на обработку персональных данных: когда и какую редакцию принял.
-- NULL у всех, кто зарегистрировался до галочки: им приложение покажет окно
-- согласия при входе.
ALTER TABLE users ADD COLUMN IF NOT EXISTS pd_consent_at TIMESTAMPTZ NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS pd_consent_version INT NULL;

-- Паспорт. Всё, что идентифицирует человека, — одним шифротекстом
-- (AES-256-GCM, ключ PASSPORT_ENC_KEY из окружения): база без ключа паспортов
-- не раскрывает, а искать по номеру паспорта не нужно. Фото — зашифрованный
-- файл в PASSPORTS_DIR, вне uploads: маршрут /uploads до него не дотягивается.
CREATE TABLE IF NOT EXISTS user_passports (
    user_id     UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    data_enc    BYTEA NOT NULL,
    photo_path  TEXT NULL,
    -- OWNER — заполнил сам (при регистрации или в профиле), VERIFICATION —
    -- модератор при верификации, ADMIN — администратор в карточке.
    source      VARCHAR(16) NOT NULL CHECK (source IN ('OWNER', 'VERIFICATION', 'ADMIN')),
    entered_by  UUID NULL REFERENCES users(id),
    key_version INT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Кто и когда смотрел или менял паспорт. user_id без внешнего ключа: запись о
-- доступе переживает удаление паспорта и даже пользователя.
CREATE TABLE IF NOT EXISTS passport_access_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL,
    viewer_id  UUID NOT NULL REFERENCES users(id),
    action     VARCHAR(16) NOT NULL CHECK (action IN ('VIEW', 'VIEW_PHOTO', 'WRITE', 'DELETE')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_passport_access_log_user ON passport_access_log (user_id, created_at DESC);

INSERT INTO system_settings (key, value) VALUES ('pd_consent_version', '1')
ON CONFLICT (key) DO NOTHING;

-- Модератор ставит «проверенный» и смотрит паспорт, но не правит его: правка —
-- право администратора.
INSERT INTO role_permissions (role_code, permission) VALUES
    ('MODERATOR', 'checks.view'),
    ('MODERATOR', 'checks.edit'),
    ('MODERATOR', 'passports.view')
ON CONFLICT DO NOTHING;
