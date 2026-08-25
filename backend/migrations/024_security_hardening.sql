-- Migration 024: constraints and columns required by the security hardening pass.

-- 1. NOTE: there is deliberately no CHECK (balance >= 0) on users.balance.
--    An executor balance is allowed to go negative through fines — that is what
--    min_balance_limit (a negative floor for taking new orders) exists for.
--    Customer-facing debits (order holds) are guarded instead by the conditional
--    UPDATE in TransactionRepository.Debit, which subtracts only when the
--    balance covers the amount.

-- 2. Password reset codes are rate limited by attempt count.
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_reset_attempts INT NOT NULL DEFAULT 0;

-- 3. An order hold may not be negative. Existing rows are normalised first: a
--    constraint that fails on live data would abort the whole migration and, on
--    a deploy that migrates at start-up, keep the service from booting.
UPDATE orders SET hold_amount = 0 WHERE hold_amount < 0;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_hold_amount_non_negative;
ALTER TABLE orders ADD CONSTRAINT orders_hold_amount_non_negative CHECK (hold_amount >= 0);

-- 4. Only one active shift per executor. StartShift checks this in code, but the
--    check and the insert are not atomic without the index.
--    Executors that somehow ended up with several open shifts keep the most
--    recent one; the older ones are closed as completed, without a penalty.
UPDATE shifts s
SET status = 'COMPLETED', actual_end_at = COALESCE(actual_end_at, now())
WHERE s.status = 'ACTIVE'
  AND EXISTS (
      SELECT 1 FROM shifts newer
      WHERE newer.executor_id = s.executor_id
        AND newer.status = 'ACTIVE'
        AND (newer.started_at, newer.id) > (s.started_at, s.id)
  );

CREATE UNIQUE INDEX IF NOT EXISTS idx_shifts_one_active_per_executor
    ON shifts (executor_id) WHERE status = 'ACTIVE';

-- 5. Only one pending withdrawal request per user: requests do not reserve
--    funds, so several open ones for the same balance cannot all be honoured.
--    This is enforced in AdminService.CreateWithdrawalRequest rather than by a
--    unique index: closing an existing request to satisfy a constraint would
--    silently reject somebody's payout, and approval re-checks the balance
--    under a row lock anyway, so a duplicate request can never overdraw.

-- 6. One bid per executor per order.
DELETE FROM bids a USING bids b
    WHERE a.order_id = b.order_id AND a.executor_id = b.executor_id AND a.ctid < b.ctid;
CREATE UNIQUE INDEX IF NOT EXISTS idx_bids_one_per_executor_per_order
    ON bids (order_id, executor_id);

-- NOTE: the indexes backing the attachment authorization check live in
-- migration 025, next to the CREATE TABLE for support_messages. On a fresh
-- database that table does not exist yet at this point.
