-- Migration 029: system accounts, so that money has a counterparty.
--
-- Until now money only ever moved on one side. A fine left the executor's
-- balance and stopped existing; a hold left the customer's balance and lived
-- only as orders.hold_amount; a top-up appeared from nowhere. That makes the
-- books unclosable: there is no statement of where the platform's money is.
--
-- Each account below is one side of a movement that used to be missing:
--
--   ESCROW    money held from customers for orders that are still running
--   FINES     penalties collected from executors
--   DEPOSITS  the outside world money enters from (top-ups)
--   PAYOUTS   money reserved for withdrawals, leaving the system on approval
--
-- Balances are kept as running totals, and every entry in transactions now
-- records which account it faced, so the totals can be re-derived and checked.

CREATE TABLE IF NOT EXISTS system_accounts (
    code       VARCHAR(32) PRIMARY KEY,
    name       VARCHAR(128) NOT NULL,
    balance    NUMERIC(18,2) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO system_accounts (code, name) VALUES
    ('ESCROW',   'Удержано по активным заказам'),
    ('FINES',    'Собранные штрафы'),
    ('DEPOSITS', 'Внешний мир: поступления и выплаты'),
    ('PAYOUTS',  'Зарезервировано на выплаты')
ON CONFLICT (code) DO NOTHING;

-- The account on the other side of a user-facing entry. NULL on historical rows,
-- which is why reconciliation reports the two eras separately.
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS counterparty VARCHAR(32) NULL
    REFERENCES system_accounts(code);

CREATE INDEX IF NOT EXISTS idx_transactions_counterparty
    ON transactions (counterparty) WHERE counterparty IS NOT NULL;

-- Seeding. The books have to close from the moment this migration runs, so the
-- accounts start from what the system actually holds right now:
--
--   ESCROW    what live orders are holding
--   PAYOUTS   what pending withdrawal requests have reserved
--   FINES     what was collected historically (those entries exist in the log
--             even though the money had nowhere to go at the time)
--   DEPOSITS  the balancing figure — everything users hold plus everything the
--             platform holds came in from outside at some point
--
-- After this, the invariant sum(user balances) + sum(system accounts) = 0 holds
-- and every later movement keeps it, because the ledger always moves two sides.

UPDATE system_accounts SET balance = COALESCE((
        SELECT SUM(hold_amount) FROM orders
        WHERE status IN ('SEARCHING', 'ASSIGNED', 'EXECUTED') AND hold_amount > 0
    ), 0), updated_at = now()
WHERE code = 'ESCROW';

UPDATE system_accounts SET balance = COALESCE((
        SELECT SUM(amount) FROM balance_withdrawal_requests WHERE status = 'PENDING'
    ), 0), updated_at = now()
WHERE code = 'PAYOUTS';

UPDATE system_accounts SET balance = COALESCE((
        SELECT SUM(amount) FROM transactions WHERE type = 'FINE'
    ), 0), updated_at = now()
WHERE code = 'FINES';

UPDATE system_accounts SET balance = -(
        COALESCE((SELECT SUM(balance) FROM users), 0)
        + COALESCE((SELECT SUM(balance) FROM system_accounts WHERE code <> 'DEPOSITS'), 0)
    ), updated_at = now()
WHERE code = 'DEPOSITS';
