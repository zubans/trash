-- +migrate no-transaction
-- 052_soft_banned_and_disputed_statuses.sql
-- New enum values: a user status between ACTIVE and BANNED, an order status for
-- an execution the customer disputes, and a transaction type for the payout
-- the platform funds when an arbitrator cannot tell who is right.
--
-- The migration runs OUTSIDE a transaction, and it holds only enum values on
-- purpose: a value added by "ALTER TYPE ... ADD VALUE" cannot be used until the
-- statement that added it has committed. Tables, defaults and checks that
-- mention these values therefore live in 053 and later, never here. Every
-- statement is idempotent, so a re-run is safe.
--
-- SOFT_BANNED: the account is blocked, but the user can still sign in — to see
-- why, to write to support and to finish orders already in progress. BANNED
-- keeps meaning "no sign-in at all".
--
-- DISPUTED: the executor marked the order executed and the customer says it was
-- not. It sits right after EXECUTED, the only status a dispute is opened from.

ALTER TYPE status_type ADD VALUE IF NOT EXISTS 'SOFT_BANNED' AFTER 'ACTIVE';

ALTER TYPE order_status_type ADD VALUE IF NOT EXISTS 'DISPUTED' AFTER 'EXECUTED';

-- DISPUTE_REWARD: the executor's payout on an "unknown" arbitration decision.
-- The customer gets the whole hold back, so no customer money funds it: it comes
-- from the DISPUTES account (053), which goes negative like BONUSES does.
ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'DISPUTE_REWARD';
