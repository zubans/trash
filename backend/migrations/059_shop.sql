-- 059_shop.sql
-- Shop tables: products, purchases, perks and pickup points.
--
-- A shop product is a price over something the platform already knows how to
-- issue (see implementation_plan_shop.md): merch and certificates are issued
-- as achievement gifts (giftRepo.Issue), only paid for; the perk is the one
-- genuinely new mechanism. That is why shop_products references gifts(code)
-- for everything that is not a PERK, and why the stock is shared between
-- achievements and the shop — one shelf, not two ledgers.
--
-- Unlike 058_shop_money.sql this migration runs inside a transaction: it only
-- creates tables and columns, and all of it either succeeds together or not
-- at all.

CREATE TABLE shop_products (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind                VARCHAR(16) NOT NULL CHECK (kind IN ('PERK', 'PHYSICAL', 'CERTIFICATE')),
    category            VARCHAR(32) NOT NULL,
    title               JSONB NOT NULL,
    description         JSONB NOT NULL DEFAULT '{}'::jsonb,
    images              JSONB NOT NULL DEFAULT '[]'::jsonb,        -- ["/uploads/shop/…"]
    price               BIGINT NOT NULL CHECK (price > 0),          -- копейки
    compare_at_price    BIGINT NULL,
    roles               TEXT[] NOT NULL DEFAULT '{}',               -- пусто = все
    requires_verified   BOOLEAN NOT NULL DEFAULT FALSE,
    per_user_limit      INT NULL,                                   -- NULL = без лимита
    max_qty_per_order   INT NOT NULL DEFAULT 1,
    -- PHYSICAL / CERTIFICATE
    gift_code           VARCHAR(64) NULL REFERENCES gifts(code),
    variants            JSONB NOT NULL DEFAULT '[]'::jsonb,         -- [{"code":"M","title":{…}}]
    fulfillment_methods TEXT[] NOT NULL DEFAULT '{}',               -- PICKUP, DELIVERY
    -- PERK
    perk_kind           VARCHAR(32) NULL CHECK (perk_kind IN
                            ('COMMISSION_MULTIPLIER', 'COMMISSION_DISCOUNT_PP', 'COMMISSION_FREE')),
    -- The meaning of the value is set by the kind: multiplier (0;1], discount
    -- points (>0); a commission-free period has no value at all.
    perk_value          NUMERIC(6,4) NULL,
    perk_days           INT NULL CHECK (perk_days > 0),
    max_active_per_user INT NULL,
    sort_order          INT NOT NULL DEFAULT 0,
    is_active           BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (kind <> 'PERK' OR (perk_kind IS NOT NULL AND perk_days IS NOT NULL)),
    -- И обратно: у привилегии нет подарка, а у вещи и сертификата — полей
    -- привилегии. Поля чужого рода — это всегда ошибка ввода, и молча хранить
    -- их — значит показывать в админке то, что не работает.
    CHECK (kind <> 'PERK' OR gift_code IS NULL),
    CHECK (kind = 'PERK' OR (perk_kind IS NULL AND perk_value IS NULL
                             AND perk_days IS NULL AND max_active_per_user IS NULL)),
    -- Значение обязано подходить своему виду: база не пропускает товар,
    -- который в Go пришлось бы отбрасывать инцидентом.
    CHECK (perk_kind IS NULL
           OR (perk_kind = 'COMMISSION_MULTIPLIER' AND perk_value > 0 AND perk_value <= 1)
           OR (perk_kind = 'COMMISSION_DISCOUNT_PP' AND perk_value > 0)
           OR (perk_kind = 'COMMISSION_FREE' AND perk_value IS NULL)),
    CHECK (kind = 'PERK' OR gift_code IS NOT NULL),
    -- Денежные и количественные поля не бывают отрицательными, а «старая цена»
    -- без превышения над ценой зачёркивать нечего.
    CHECK (compare_at_price IS NULL OR compare_at_price > price),
    CHECK (max_qty_per_order > 0),
    CHECK (per_user_limit IS NULL OR per_user_limit > 0),
    CHECK (max_active_per_user IS NULL OR max_active_per_user > 0)
);

CREATE TABLE shop_orders (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    number          BIGSERIAL UNIQUE,                   -- «Заказ №1042» для людей
    user_id         UUID NOT NULL REFERENCES users(id),
    request_id      UUID NOT NULL,
    product_id      UUID NOT NULL REFERENCES shop_products(id),
    product_snapshot JSONB NOT NULL,                    -- название, род, параметры привилегии
    variant         VARCHAR(32) NULL,
    quantity        INT NOT NULL CHECK (quantity > 0),
    unit_price      BIGINT NOT NULL CHECK (unit_price > 0),
    total           BIGINT NOT NULL,
    status          VARCHAR(16) NOT NULL
                    CHECK (status IN ('PAID', 'PROCESSING', 'SHIPPED', 'COMPLETED', 'CANCELED')),
    fulfillment     JSONB NOT NULL DEFAULT '{}'::jsonb, -- способ, пункт/адрес, трек
    refunded_amount BIGINT NOT NULL DEFAULT 0,
    offer_version   INT NOT NULL,                       -- принятая редакция оферты (§4.5)
    cancel_reason   TEXT NULL,
    canceled_by     UUID NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, request_id),
    -- Итог — это цена, умноженная на количество, а возвраты не превышают
    -- уплаченного: оба правила дешевле держать в базе, чем искать расхождение
    -- сверкой после факта.
    CHECK (total = unit_price * quantity),
    CHECK (refunded_amount >= 0 AND refunded_amount <= total)
);
CREATE INDEX idx_shop_orders_user ON shop_orders (user_id, created_at DESC);
CREATE INDEX idx_shop_orders_status ON shop_orders (status, created_at);

-- Купоны покупки: одна строка user_gifts на единицу товара.
ALTER TABLE user_gifts ADD COLUMN IF NOT EXISTS shop_order_id UUID NULL REFERENCES shop_orders(id);

-- Проводки магазина ссылаются на покупку: transactions.order_id занят заказами
-- и ссылается на orders, поэтому без своей колонки по покупке не найти ни её
-- оплату, ни её возвраты — а без этого не работают карточка покупки с
-- проводками и контроль «возвращено не больше уплаченного».
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS shop_order_id UUID NULL REFERENCES shop_orders(id);
CREATE INDEX IF NOT EXISTS idx_transactions_shop_order
    ON transactions (shop_order_id) WHERE shop_order_id IS NOT NULL;

CREATE TABLE user_perks (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          VARCHAR(32) NOT NULL,
    value         NUMERIC(6,4) NULL,                     -- NULL у COMMISSION_FREE
    starts_at     TIMESTAMPTZ NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    shop_order_id UUID NULL REFERENCES shop_orders(id),  -- NULL — выдана админом
    revoked_at    TIMESTAMPTZ NULL,
    revoked_by    UUID NULL REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expires_at > starts_at),
    -- Те же правила, что у товара: ручная выдача привилегии пишет сюда напрямую,
    -- и множитель 1.5 база принимать не должна — иначе в Go его пришлось бы
    -- отбрасывать денежным инцидентом при каждом подтверждении заказа.
    CHECK (kind IN ('COMMISSION_MULTIPLIER', 'COMMISSION_DISCOUNT_PP', 'COMMISSION_FREE')),
    CHECK ((kind = 'COMMISSION_MULTIPLIER' AND value > 0 AND value <= 1)
           OR (kind = 'COMMISSION_DISCOUNT_PP' AND value > 0)
           OR (kind = 'COMMISSION_FREE' AND value IS NULL))
);
CREATE INDEX idx_user_perks_active ON user_perks (user_id, kind, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE shop_pickup_points (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title     JSONB NOT NULL,
    address   TEXT NOT NULL,
    hours     TEXT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

ALTER TABLE orders ADD COLUMN IF NOT EXISTS commission_perk_id UUID NULL REFERENCES user_perks(id);

-- Everything starts switched off: opening the shop and setting prices is the
-- administrator's decision, not the migration's.
INSERT INTO system_settings (key, value) VALUES ('shop_enabled', '0'), ('shop_offer_version', '1')
ON CONFLICT (key) DO NOTHING;
