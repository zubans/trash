-- 034_order_tips.sql
-- Tips: a customer may tip the executor after a completed order.
--
-- The money moves customer -> ESCROW -> executor inside one transaction, so it
-- mirrors the order flow and keeps the ledger balanced. Two new transaction
-- types carry the two sides:
--
--   TIP         the customer's balance is debited (sign -1)
--   TIP_REWARD  the executor's balance is credited (sign +1)
--
-- The pair nets to zero on ESCROW, so no escrow drift is introduced.

ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'TIP';
ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'TIP_REWARD';

-- A tip is charged at most once per order; the service checks for an existing
-- TIP row before charging. Index that lookup.
CREATE INDEX IF NOT EXISTS idx_transactions_order_type
    ON transactions (order_id, type);
