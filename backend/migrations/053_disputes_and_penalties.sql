-- 053_disputes_and_penalties.sql
-- Disputes over executed orders, the penalty point journal and the state the
-- points put a user in.
--
-- Plan: doc/implementation_plan_disputes_penalties_photo_proof.md.
--
-- A dispute is opened by the customer on an EXECUTED order and closed by
-- whichever comes first: the customer confirming the order, the executor
-- conceding, or an arbitrator's decision. Only the decision awards penalty
-- points.
--
-- Points are a journal, not a counter. A counter cannot say what a point was for,
-- cannot take back one wrong point without guessing the rest, and forgets
-- everything when it expires. The journal keeps every row; revoking and expiring
-- are marks on rows. user_penalty_status is the journal folded into what the
-- request path needs to read cheaply — it is recomputed in the same transaction
-- that changes the journal, so the two never disagree.

-- =============================================================================
-- 1. Disputes
-- =============================================================================

CREATE TABLE IF NOT EXISTS order_disputes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    -- Both sides are copied from the order: the arbitration queue filters by
    -- them, and a dispute must still name its executor if the order is later
    -- reassigned or cancelled.
    customer_id UUID NOT NULL REFERENCES users(id),
    executor_id UUID NOT NULL REFERENCES users(id),
    claim       TEXT NOT NULL CHECK (btrim(claim) <> ''),
    status      VARCHAR(16) NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'CLOSED')),
    -- How the dispute ended. Only ARBITRATION carries a decision.
    closure     VARCHAR(32) NULL
        CHECK (closure IN ('CUSTOMER_CONFIRMED', 'EXECUTOR_CONCEDED', 'ARBITRATION')),
    decision    VARCHAR(16) NULL CHECK (decision IN ('EXECUTOR', 'CUSTOMER', 'UNKNOWN')),
    -- The arbitrator's note, shown to both sides with the decision.
    resolution_note TEXT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at   TIMESTAMPTZ NULL,
    closed_by   UUID NULL REFERENCES users(id),

    CONSTRAINT order_disputes_closed_consistent
        CHECK ((status = 'OPEN') = (closed_at IS NULL AND closure IS NULL)),
    CONSTRAINT order_disputes_decision_only_by_arbitration
        CHECK ((decision IS NOT NULL) = (closure IS NOT DISTINCT FROM 'ARBITRATION'))
);

-- One open dispute per order: a second "not executed" on the same order is the
-- same complaint, not a new case.
CREATE UNIQUE INDEX IF NOT EXISTS idx_order_disputes_open
    ON order_disputes (order_id)
    WHERE status = 'OPEN';

CREATE INDEX IF NOT EXISTS idx_order_disputes_status
    ON order_disputes (status, created_at);

CREATE INDEX IF NOT EXISTS idx_order_disputes_executor
    ON order_disputes (executor_id, created_at);

CREATE INDEX IF NOT EXISTS idx_order_disputes_customer
    ON order_disputes (customer_id, created_at);

-- =============================================================================
-- 2. Penalty point journal
-- =============================================================================

CREATE TABLE IF NOT EXISTS penalty_points (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Points of a person's executor and customer roles never add up: they are
    -- different conduct with different consequences.
    role        VARCHAR(16) NOT NULL CHECK (role IN ('CUSTOMER', 'EXECUTOR')),
    order_id    UUID NULL REFERENCES orders(id) ON DELETE SET NULL,
    dispute_id  UUID NULL REFERENCES order_disputes(id) ON DELETE SET NULL,
    -- Who awarded the point. NULL only for points the system awards by itself.
    assigned_by UUID NULL REFERENCES users(id),
    reason      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Taken back by an administrator: the point was a mistake.
    revoked_at  TIMESTAMPTZ NULL,
    revoked_by  UUID NULL REFERENCES users(id),
    -- Burnt: no new point for long enough, or a silent block has run out.
    expired_at  TIMESTAMPTZ NULL,

    CONSTRAINT penalty_points_revoked_consistent
        CHECK ((revoked_at IS NULL) = (revoked_by IS NULL))
);

-- A dispute awards at most one point per side. The decision is taken once, but a
-- retried request must not turn one decision into two points.
CREATE UNIQUE INDEX IF NOT EXISTS idx_penalty_points_dispute_side
    ON penalty_points (dispute_id, user_id, role)
    WHERE dispute_id IS NOT NULL;

-- The only question the request path asks the journal: the live points of one
-- role of one user.
CREATE INDEX IF NOT EXISTS idx_penalty_points_live
    ON penalty_points (user_id, role, created_at)
    WHERE revoked_at IS NULL AND expired_at IS NULL;

-- =============================================================================
-- 3. Folded state
-- =============================================================================

-- Per role: what the live points of that role currently mean.
CREATE TABLE IF NOT EXISTS user_penalty_status (
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role                    VARCHAR(16) NOT NULL CHECK (role IN ('CUSTOMER', 'EXECUTOR')),
    active_points           INT NOT NULL DEFAULT 0 CHECK (active_points >= 0),
    -- Orders taken before this moment need a photo proof. NULL — no period.
    photo_required_until    TIMESTAMPTZ NULL,
    -- A silent block ends on its own after silent_block_months, whatever the
    -- points do in the meantime, so its end is stored rather than derived.
    silent_block_started_at TIMESTAMPTZ NULL,
    silent_block_ends_at    TIMESTAMPTZ NULL,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role),

    CONSTRAINT user_penalty_status_silent_block_consistent
        CHECK ((silent_block_started_at IS NULL) = (silent_block_ends_at IS NULL))
);

-- The worker's two scans: blocks that have run out and periods that have ended.
CREATE INDEX IF NOT EXISTS idx_user_penalty_status_silent_block_end
    ON user_penalty_status (silent_block_ends_at)
    WHERE silent_block_ends_at IS NOT NULL;

-- Per user: facts that outlive any single role's points.
CREATE TABLE IF NOT EXISTS user_penalty_flags (
    user_id                  UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- Set when a silent block is lifted and kept for good: the next point in any
    -- role is a relapse and turns the account SOFT_BANNED. Only an administrator
    -- clears it.
    had_silent_block_at      TIMESTAMPTZ NULL,
    -- Why and when the account went SOFT_BANNED. soft_banned_by is NULL when the
    -- system did it on a relapse, and set when an administrator did it by hand.
    soft_banned_at           TIMESTAMPTZ NULL,
    soft_banned_by           UUID NULL REFERENCES users(id),
    soft_ban_reason          TEXT NULL,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- =============================================================================
-- 4. Money
-- =============================================================================

-- DISPUTES funds the executor's payout on an "unknown" decision, when the
-- customer gets the whole hold back. Like BONUSES it is an expense account and
-- goes negative; its balance is what unresolvable disputes have cost.
INSERT INTO system_accounts (code, name) VALUES
    ('DISPUTES', 'Выплаты по спорам с неизвестным исходом')
ON CONFLICT (code) DO NOTHING;

-- =============================================================================
-- 5. Settings
-- =============================================================================

INSERT INTO system_settings (key, value) VALUES
    -- N: this many live points of a role turn on the photo proof period; twice
    -- as many turn on the silent block.
    ('penalty_points_threshold', '2'),
    -- Length of the photo proof period, and how far a new point extends it.
    ('photo_requirement_months', '3'),
    -- Months without a new point after which a role's points burn.
    ('penalty_points_ttl_months', '3'),
    -- Length of a silent block. It is not shortened by points burning.
    ('silent_block_months', '6'),
    -- Arbitration highlights a photo taken further than this from the "executed"
    -- mark in time, or from the order address in space.
    ('photo_proof_max_time_diff_min', '30'),
    ('photo_proof_max_distance_m', '300')
ON CONFLICT (key) DO NOTHING;

-- =============================================================================
-- 6. Permissions
-- =============================================================================

-- The moderator already handles the checks that need a human; disputes are the
-- same kind of work. ADMIN needs no rows: it passes every check in code.
INSERT INTO role_permissions (role_code, permission) VALUES
    ('MODERATOR', 'disputes.view'),
    ('MODERATOR', 'disputes.edit'),
    ('MODERATOR', 'penalties.edit')
ON CONFLICT DO NOTHING;

-- =============================================================================
-- 7. Achievement
-- =============================================================================

-- "First repentance": an executor who concedes a dispute before arbitration
-- saves everyone a case. Switched on here, unlike the achievements of 047: the
-- business decided to award it, so there is no decision left for an
-- administrator to take. The script ships with the binary.
INSERT INTO achievements (code, is_active, sort_order) VALUES
    ('first_repentance', TRUE, 40)
ON CONFLICT (code) DO NOTHING;
