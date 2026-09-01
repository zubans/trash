-- 045_order_submissions.sql
-- Data an executor submits for checking, and what happens when it does not
-- match.
--
-- The verification service needs a step the platform had no shape for: the
-- moderator sees the address and nothing else about the customer, types in what
-- the passport says, and the system — not the moderator — decides whether it
-- matches. A mismatch is not a failure to hide: it is a warning, and on the
-- next attempt the case goes to an administrator.
--
-- Both tables are deliberately generic. A submission is "the executor sent these
-- fields for this order and this is how they compared"; an escalation is "this
-- order needs a human above the executor". Which fields are compared, how many
-- attempts there are and when to escalate live in the behaviour script.

CREATE TABLE IF NOT EXISTS order_submissions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    executor_id UUID NOT NULL REFERENCES users(id),
    attempt     INT NOT NULL,
    matched     BOOLEAN NOT NULL,
    -- What was typed. Kept because an administrator reviewing an escalation has
    -- to see what the moderator read off the document.
    fields      JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- Which of the checked fields did not match. The values they were compared
    -- against are never stored here: the customer's own record holds them.
    mismatches  TEXT[] NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_order_submissions_order
    ON order_submissions (order_id, created_at);

CREATE TABLE IF NOT EXISTS behavior_escalations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id      UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    behavior_code VARCHAR(64) NOT NULL,
    reason        TEXT NOT NULL,
    status        VARCHAR(16) NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'RESOLVED')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at   TIMESTAMPTZ NULL,
    resolved_by   UUID NULL REFERENCES users(id)
);

-- One open escalation per order: a script that asks twice is describing the same
-- case, not a second one.
CREATE UNIQUE INDEX IF NOT EXISTS idx_behavior_escalations_open
    ON behavior_escalations (order_id)
    WHERE status = 'OPEN';

CREATE INDEX IF NOT EXISTS idx_behavior_escalations_status
    ON behavior_escalations (status, created_at);
