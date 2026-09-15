-- +migrate no-transaction
-- 052_soft_banned_and_disputed_statuses.sql
-- Two new enum values: a user status between ACTIVE and BANNED, and an order
-- status for an execution the customer disputes.
--
-- The migration runs OUTSIDE a transaction, and it is alone in its file on
-- purpose: a value added by "ALTER TYPE ... ADD VALUE" cannot be used until the
-- statement that added it has committed. Tables, defaults and checks that
-- mention SOFT_BANNED or DISPUTED therefore live in 053 and later, never here.
-- Both statements are idempotent, so a re-run is safe.
--
-- SOFT_BANNED: the account is blocked, but the user can still sign in — to see
-- why, to write to support and to finish orders already in progress. BANNED
-- keeps meaning "no sign-in at all".
--
-- DISPUTED: the executor marked the order executed and the customer says it was
-- not. It sits right after EXECUTED, the only status a dispute is opened from.

ALTER TYPE status_type ADD VALUE IF NOT EXISTS 'SOFT_BANNED' AFTER 'ACTIVE';

ALTER TYPE order_status_type ADD VALUE IF NOT EXISTS 'DISPUTED' AFTER 'EXECUTED';
