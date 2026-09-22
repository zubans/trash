-- 062_perk_rules.sql
-- Правила привилегий — скрипты, а не виды, зашитые в Go
-- (implementation_plan_delivery_passport.md §1).
--
-- Было: shop_products.perk_kind/perk_value и user_perks.kind/value, а формула —
-- switch в ApplyPerk. Стало: товар ссылается на правило и его константы,
-- привилегия — на правило, версию и снимок констант на момент покупки.
--
-- Поставляемые правила (commission_multiplier, commission_discount_pp,
-- commission_free) живут в бинарнике и версионируются сборкой: их текст меняет
-- только релиз, прошедший ревью. Поэтому у привилегии на поставляемом правиле
-- rule_version_id пуст. Собственное правило админа правится в админке, и каждая
-- правка — новая версия: купленная привилегия считается тем текстом, который
-- был продан.

CREATE TABLE IF NOT EXISTS perk_rules (
    code       VARCHAR(64) PRIMARY KEY,
    title      TEXT NOT NULL,
    origin     VARCHAR(8) NOT NULL CHECK (origin IN ('SHIPPED', 'OWN')),
    is_active  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Версии собственных правил. Строка неизменяема; текущая версия правила —
-- последняя по времени. Хеш не уникален: возврат к прошлому тексту — новая
-- версия, иначе текущей осталась бы та, от которой отказались.
CREATE TABLE IF NOT EXISTS perk_rule_versions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_code  VARCHAR(64) NOT NULL REFERENCES perk_rules(code),
    hash       CHAR(64) NOT NULL,
    source     TEXT NOT NULL,
    created_by UUID NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_perk_rule_versions_latest ON perk_rule_versions (rule_code, created_at DESC);

INSERT INTO perk_rules (code, title, origin) VALUES
    ('commission_multiplier',  'Комиссия умножается на VALUE', 'SHIPPED'),
    ('commission_discount_pp', 'Комиссия минус VALUE пунктов', 'SHIPPED'),
    ('commission_free',        'Без комиссии',                 'SHIPPED')
ON CONFLICT (code) DO NOTHING;

-- Товары. Удаление perk_kind/perk_value снимает и CHECK, которые на них
-- ссылались: смысл значения теперь задаёт правило, а границы проверяет прогон
-- по сетке при сохранении товара.
ALTER TABLE shop_products ADD COLUMN IF NOT EXISTS perk_rule VARCHAR(64) NULL REFERENCES perk_rules(code);
ALTER TABLE shop_products ADD COLUMN IF NOT EXISTS perk_config JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE shop_products SET
    perk_rule = CASE perk_kind
        WHEN 'COMMISSION_MULTIPLIER'  THEN 'commission_multiplier'
        WHEN 'COMMISSION_DISCOUNT_PP' THEN 'commission_discount_pp'
        WHEN 'COMMISSION_FREE'        THEN 'commission_free'
    END,
    perk_config = CASE WHEN perk_value IS NULL THEN '{}'::jsonb
                       ELSE jsonb_build_object('VALUE', perk_value) END
WHERE perk_kind IS NOT NULL;

ALTER TABLE shop_products DROP COLUMN IF EXISTS perk_kind;
ALTER TABLE shop_products DROP COLUMN IF EXISTS perk_value;
ALTER TABLE shop_products DROP CONSTRAINT IF EXISTS shop_products_perk_fields;
ALTER TABLE shop_products DROP CONSTRAINT IF EXISTS shop_products_no_perk_fields;
ALTER TABLE shop_products ADD CONSTRAINT shop_products_perk_fields
    CHECK (kind <> 'PERK' OR (perk_rule IS NOT NULL AND perk_days IS NOT NULL));
ALTER TABLE shop_products ADD CONSTRAINT shop_products_no_perk_fields
    CHECK (kind = 'PERK' OR (perk_rule IS NULL AND perk_config = '{}'::jsonb
                             AND perk_days IS NULL AND max_active_per_user IS NULL));

-- Привилегии пользователей.
ALTER TABLE user_perks ADD COLUMN IF NOT EXISTS rule_code VARCHAR(64) NULL REFERENCES perk_rules(code);
ALTER TABLE user_perks ADD COLUMN IF NOT EXISTS rule_version_id UUID NULL REFERENCES perk_rule_versions(id);
ALTER TABLE user_perks ADD COLUMN IF NOT EXISTS config JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE user_perks SET
    rule_code = CASE kind
        WHEN 'COMMISSION_MULTIPLIER'  THEN 'commission_multiplier'
        WHEN 'COMMISSION_DISCOUNT_PP' THEN 'commission_discount_pp'
        WHEN 'COMMISSION_FREE'        THEN 'commission_free'
    END,
    config = CASE WHEN value IS NULL THEN '{}'::jsonb
                  ELSE jsonb_build_object('VALUE', value) END
WHERE rule_code IS NULL;

ALTER TABLE user_perks ALTER COLUMN rule_code SET NOT NULL;
-- Вместе с колонкой уходит и индекс по ней; очередь теперь общая на все правила.
ALTER TABLE user_perks DROP COLUMN IF EXISTS kind;
ALTER TABLE user_perks DROP COLUMN IF EXISTS value;
CREATE INDEX IF NOT EXISTS idx_user_perks_queue ON user_perks (user_id, expires_at) WHERE revoked_at IS NULL;

-- Товары по решению T0.1 — выключенными: открыть их продажу решает админ.
INSERT INTO shop_products (id, kind, category, title, description, price, roles,
                           perk_rule, perk_config, perk_days, is_active, sort_order)
VALUES
    ('8f1d6c52-0b1e-4d0e-9a55-000000000001', 'PERK', 'perks',
     '{"ru": "Комиссия вдвое меньше", "en": "Half commission"}',
     '{"ru": "30 дней ставка комиссии вдвое ниже вашей ставки по уровню."}',
     100000, '{EXECUTOR}', 'commission_multiplier', '{"VALUE": 0.5}', 30, FALSE, 10),
    ('8f1d6c52-0b1e-4d0e-9a55-000000000002', 'PERK', 'perks',
     '{"ru": "Комиссия минус 5 пунктов", "en": "Commission minus 5 points"}',
     '{"ru": "30 дней из вашей ставки по уровню вычитается 5 процентных пунктов."}',
     100000, '{EXECUTOR}', 'commission_discount_pp', '{"VALUE": 5}', 30, FALSE, 20),
    ('8f1d6c52-0b1e-4d0e-9a55-000000000003', 'PERK', 'perks',
     '{"ru": "День без комиссии", "en": "A day without commission"}',
     '{"ru": "Сутки комиссия 0 %."}',
     10000, '{EXECUTOR}', 'commission_free', '{}', 1, FALSE, 30),
    ('8f1d6c52-0b1e-4d0e-9a55-000000000004', 'PERK', 'perks',
     '{"ru": "Неделя без комиссии", "en": "A week without commission"}',
     '{"ru": "7 дней комиссия 0 %."}',
     50000, '{EXECUTOR}', 'commission_free', '{}', 7, FALSE, 40)
ON CONFLICT (id) DO NOTHING;
