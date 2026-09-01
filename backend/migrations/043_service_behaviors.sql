-- +migrate no-transaction
-- 043_service_behaviors.sql
-- Special services: behaviour scripts instead of one boolean column per rule.
--
-- Until now every unusual property of a service was a column of its own —
-- requires_verification (035), moderator_only (040), min_age — plus an `if` in
-- service/eligibility.go for each. That does not scale: the verification
-- service alone needs four rules at once (visible only to unverified users,
-- orderable once, free, pays the verifier a fixed reward), and none of them is
-- reusable as a flag.
--
-- A node may now name a behaviour. The behaviour itself lives in a Starlark
-- script outside the Go code (backend/behaviors/*.star) and is evaluated by
-- backend/behavior; the node carries only which behaviour it uses and its
-- per-node configuration. Nodes without a behaviour_code behave exactly as
-- before, so this migration changes nothing on its own.
--
-- This migration must run OUTSIDE a transaction: PostgreSQL refuses
-- "ALTER TYPE ... ADD VALUE" inside a transaction block (see 038). Every
-- statement below is individually idempotent.

-- BONUS credits a user from the platform's own pocket: the reward paid for
-- verifying somebody is not money any customer paid, so it cannot come out of
-- ESCROW. It comes from BONUSES, which goes negative exactly like DEPOSITS —
-- that negative balance IS the amount the platform has paid out in rewards.
ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'BONUS';

INSERT INTO system_accounts (code, name) VALUES
    ('BONUSES', 'Выплаченные платформой вознаграждения')
ON CONFLICT (code) DO NOTHING;

-- A ceiling on a single scripted payout, in rubles. The script decides the
-- amount; this decides how wrong a script is allowed to be. An effect above it
-- is refused and the event is left unprocessed rather than paid out.
INSERT INTO system_settings (key, value) VALUES ('behavior_max_bonus', '5000')
ON CONFLICT (key) DO NOTHING;

ALTER TABLE service_nodes ADD COLUMN IF NOT EXISTS behavior_code VARCHAR(64);
ALTER TABLE service_nodes ADD COLUMN IF NOT EXISTS behavior_config JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Transactional outbox. A state change that a behaviour has to react to is
-- written together with its event in one transaction, so "the moderator marked
-- the job done" and "the verifier gets paid" cannot come apart: either both are
-- committed or neither is. The dispatcher reads from here.
CREATE TABLE IF NOT EXISTS domain_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type          VARCHAR(64) NOT NULL,
    subject_type  VARCHAR(16) NOT NULL CHECK (subject_type IN ('user', 'order')),
    subject_id    UUID NOT NULL,
    actor_id      UUID NULL,
    payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at  TIMESTAMPTZ NULL,
    attempts      INT NOT NULL DEFAULT 0,
    last_error    TEXT NULL
);

-- The dispatcher's only query: the oldest unprocessed events. Partial, because
-- processed rows are kept as history and would otherwise dominate the index.
CREATE INDEX IF NOT EXISTS idx_domain_events_pending
    ON domain_events (created_at)
    WHERE processed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_domain_events_subject
    ON domain_events (subject_type, subject_id);

-- "Orderable once per user" as a fact in the database rather than a check in
-- the service: the claim row is inserted in the same transaction as the order,
-- so two simultaneous requests cannot both pass a "has he ordered it?" query.
-- The row is removed when the order is cancelled — a cancelled order must not
-- lock a user out of a service forever, least of all out of verification.
CREATE TABLE IF NOT EXISTS user_service_claims (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    variant_id UUID NOT NULL REFERENCES service_nodes(id),
    order_id   UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, variant_id)
);

CREATE INDEX IF NOT EXISTS idx_user_service_claims_order
    ON user_service_claims (order_id);

-- Idempotency for the effects a script asks for. A money effect must carry a
-- key, and the key is unique here: a redelivered event, a retried batch or a
-- second event describing the same outcome all collide on this row instead of
-- paying twice.
CREATE TABLE IF NOT EXISTS behavior_effects (
    idempotency_key VARCHAR(200) PRIMARY KEY,
    event_id        UUID NOT NULL REFERENCES domain_events(id) ON DELETE CASCADE,
    behavior_code   VARCHAR(64) NOT NULL,
    kind            VARCHAR(32) NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    applied_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_behavior_effects_event
    ON behavior_effects (event_id);

-- The first scripted service. Seeded switched OFF: publishing a service to
-- every unverified customer is an admin's decision, not a migration's. The
-- category carries the behaviour too, so it disappears from the catalog along
-- with its only variant instead of standing there empty.
--
-- behavior_config stays empty on purpose: the amounts, the verifier role and the
-- verification mode are constants of the behaviour itself
-- (backend/behaviors/verification/config.star), and this column holds only what
-- a particular node changes about them.
INSERT INTO service_nodes (id, parent_id, code, name, description, node_type, base_price, is_auction, is_active, sort_order, behavior_code, behavior_config) VALUES
('cccccccc-cccc-cccc-cccc-cccccccccccc', NULL, 'account_verification',
 '{"ru": "Верификация аккаунта", "en": "Account verification"}',
 '{"ru": "Подтверждение личности с выездом модератора", "en": "Identity check by a moderator"}',
 'CATEGORY', NULL, FALSE, FALSE, 90, 'verification', '{}'),
('dddddddd-dddd-dddd-dddd-dddddddddddd', 'cccccccc-cccc-cccc-cccc-cccccccccccc', 'account_verification_visit',
 '{"ru": "Верификация аккаунта", "en": "Account verification"}',
 '{"ru": "Бесплатно. Модератор подтверждает вашу личность", "en": "Free. A moderator confirms your identity"}',
 'VARIANT', 0.00, FALSE, FALSE, 1, 'verification',
 '{}')
ON CONFLICT (id) DO NOTHING;

SELECT rebuild_service_node_paths('cccccccc-cccc-cccc-cccc-cccccccccccc');
SELECT rebuild_service_node_paths('dddddddd-dddd-dddd-dddd-dddddddddddd');
