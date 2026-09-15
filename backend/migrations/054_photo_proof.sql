-- 054_photo_proof.sql
-- Photo proof of execution: the gestures an executor shows in the photo, the
-- photos themselves, and what an order remembers about the requirement.
--
-- Plan: doc/implementation_plan_disputes_penalties_photo_proof.md, section 3.
--
-- An order needs a photo proof when, at the moment it is taken, the executor or
-- the customer is in a photo proof period (053). The server picks a gesture for
-- the order right then and stores it on the order: the phone must know it
-- without a network, and arbitration compares the photo against the gesture the
-- order was given, not against whatever the list says today.

-- =============================================================================
-- 1. Gestures
-- =============================================================================

CREATE TABLE IF NOT EXISTS watermark_symbols (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code           VARCHAR(32) NOT NULL,
    -- A stable small number of the gesture. Assigned once and never reused,
    -- including after deletion: photos already taken refer to it.
    number         SMALLINT NOT NULL CHECK (number BETWEEN 0 AND 255),
    title          VARCHAR(128) NOT NULL CHECK (btrim(title) <> ''),
    -- What exactly to show, in the words the executor reads in the popup.
    description    TEXT NOT NULL DEFAULT '',
    hint_image_url TEXT NULL,
    -- Whether the gesture can be shown in a selfie with the customer. Feet cannot,
    -- so for them the selfie hint does not ask for the gesture.
    fits_in_selfie BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order     INT NOT NULL DEFAULT 0,
    -- Soft deletion: orders and photos keep pointing at a deleted gesture, it is
    -- only no longer picked for new orders.
    deleted_at     TIMESTAMPTZ NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_watermark_symbols_code_live
    ON watermark_symbols (code)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_watermark_symbols_number
    ON watermark_symbols (number);

INSERT INTO watermark_symbols (code, number, title, description, fits_in_selfie, sort_order) VALUES
    ('thumb_up', 1, 'Поднятый большой палец',
     'Кулак, большой палец поднят вверх («лайк»). Держите руку рядом с объектом заказа.', TRUE, 10),
    ('index_finger', 2, 'Указательный палец',
     'Поднят один указательный палец, остальные сжаты. Держите руку рядом с объектом заказа.', TRUE, 20),
    ('bunny', 3, 'Зайчик',
     'Подняты указательный и средний пальцы, как уши зайца. Держите руку рядом с объектом заказа.', TRUE, 30),
    ('ok', 4, 'ОК',
     'Большой и указательный пальцы соединены в кольцо, остальные подняты. Держите руку рядом с объектом заказа.', TRUE, 40),
    ('left_foot', 5, 'Левая нога',
     'В кадре рядом с объектом заказа — носок вашей левой ноги.', FALSE, 50),
    ('right_foot', 6, 'Правая нога',
     'В кадре рядом с объектом заказа — носок вашей правой ноги.', FALSE, 60)
ON CONFLICT DO NOTHING;

-- =============================================================================
-- 2. What an order remembers
-- =============================================================================

ALTER TABLE orders ADD COLUMN IF NOT EXISTS photo_required BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS watermark_symbol_id UUID NULL REFERENCES watermark_symbols(id);
-- Per-order service data of the photo check. Issued with the gesture, never
-- shown in the interface.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS proof_key BYTEA NULL;
-- When the executor pressed "executed" on the phone. It differs from the server
-- time when the mark waited in the offline queue, and arbitration shows both.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS executed_at_device TIMESTAMPTZ NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'orders_photo_required_consistent'
    ) THEN
        -- A required proof always has a gesture and a key; an order without the
        -- requirement has neither.
        ALTER TABLE orders ADD CONSTRAINT orders_photo_required_consistent
            CHECK (photo_required = (watermark_symbol_id IS NOT NULL AND proof_key IS NOT NULL));
    END IF;
END $$;

-- =============================================================================
-- 3. Photos
-- =============================================================================

CREATE TABLE IF NOT EXISTS order_photo_proofs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id         UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    executor_id      UUID NOT NULL REFERENCES users(id),
    -- AREA: the order place with the gesture, required. SELFIE: with the
    -- customer, optional.
    kind             VARCHAR(16) NOT NULL CHECK (kind IN ('AREA', 'SELFIE')),
    camera           VARCHAR(16) NOT NULL CHECK (camera IN ('FRONT', 'REAR')),
    -- The gesture the order had when the photo was taken.
    symbol_id        UUID NOT NULL REFERENCES watermark_symbols(id),
    -- Idempotency key generated on the phone: the offline queue retries an
    -- upload it cannot confirm, and the retry must not become a second photo.
    client_key       VARCHAR(64) NOT NULL,
    file_url         TEXT NOT NULL,
    file_sha256      CHAR(64) NOT NULL,
    file_size        BIGINT NOT NULL CHECK (file_size > 0),
    -- From the file's EXIF, when present.
    exif_taken_at    TIMESTAMPTZ NULL,
    exif_lat         DOUBLE PRECISION NULL,
    exif_lon         DOUBLE PRECISION NULL,
    -- From the phone at the moment of shooting.
    device_taken_at  TIMESTAMPTZ NOT NULL,
    device_lat       DOUBLE PRECISION NULL,
    device_lon       DOUBLE PRECISION NULL,
    -- Results of the photo check, set on upload.
    seal_status      VARCHAR(16) NOT NULL CHECK (seal_status IN ('VALID', 'MISSING', 'INVALID')),
    mark_status      VARCHAR(16) NOT NULL CHECK (mark_status IN ('FOUND', 'NOT_FOUND', 'MISMATCH')),
    uploaded_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One photo of each kind per order. A retake before "executed" replaces it; after
-- "executed" the set is closed, which the service enforces.
CREATE UNIQUE INDEX IF NOT EXISTS idx_order_photo_proofs_kind
    ON order_photo_proofs (order_id, kind);

CREATE UNIQUE INDEX IF NOT EXISTS idx_order_photo_proofs_client_key
    ON order_photo_proofs (order_id, client_key);
