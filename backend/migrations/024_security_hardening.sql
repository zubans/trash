-- Migration 024: constraints and columns required by the security hardening pass.

-- 1. NOTE: there is deliberately no CHECK (balance >= 0) on users.balance.
--    An executor balance is allowed to go negative through fines — that is what
--    min_balance_limit (a negative floor for taking new orders) exists for.
--    Customer-facing debits (order holds) are guarded instead by the conditional
--    UPDATE in TransactionRepository.Debit, which subtracts only when the
--    balance covers the amount.

-- 2. Password reset codes are rate limited by attempt count.
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_reset_attempts INT NOT NULL DEFAULT 0;

-- 3. An order hold may not be negative either.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_hold_amount_non_negative;
ALTER TABLE orders ADD CONSTRAINT orders_hold_amount_non_negative CHECK (hold_amount >= 0);

-- 4. Only one active shift per executor. StartShift checks this in code, but the
--    check and the insert are not atomic without the index.
CREATE UNIQUE INDEX IF NOT EXISTS idx_shifts_one_active_per_executor
    ON shifts (executor_id) WHERE status = 'ACTIVE';

-- 5. Only one pending withdrawal request per user: requests do not reserve
--    funds, so several open ones for the same balance cannot all be honoured.
CREATE UNIQUE INDEX IF NOT EXISTS idx_withdrawals_one_pending_per_user
    ON balance_withdrawal_requests (user_id) WHERE status = 'PENDING';

-- 6. One bid per executor per order.
DELETE FROM bids a USING bids b
    WHERE a.order_id = b.order_id AND a.executor_id = b.executor_id AND a.ctid < b.ctid;
CREATE UNIQUE INDEX IF NOT EXISTS idx_bids_one_per_executor_per_order
    ON bids (order_id, executor_id);

-- 7. Lookups used by the attachment authorization check.
CREATE INDEX IF NOT EXISTS idx_messages_file_url ON messages (file_url) WHERE file_url IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_support_messages_file_url ON support_messages (file_url) WHERE file_url IS NOT NULL;
