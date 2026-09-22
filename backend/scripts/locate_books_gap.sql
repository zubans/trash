-- Read-only diagnosis: where are the books open?
--
-- Reached for when repair_books_gap.sql refuses. That script knows one cause —
-- a movement that touched the user side and not the account side — and it
-- declines rather than guess when the evidence does not fit.
--
-- This one localises the drift instead. Three of the four accounts have a
-- definition checkable against the data they claim to summarise: ESCROW is what
-- live orders hold, FINES is every fine ever charged, PAYOUTS is what pending
-- withdrawals reserved. DEPOSITS is the balancing figure from migration 029 and
-- has no independent definition, so it is answered by elimination — if the
-- other three agree with themselves, the drift is there, meaning money entered
-- or left the outside world without a matching movement on a user balance.
--
-- The eras are printed first because they decide what the rest can be trusted
-- for. Both boundaries come from schema_migrations, not from the data: 029 is
-- where the accounts were seeded and the books closed, 060 is where
-- transactions.counterparty started being written. Between the two a NULL
-- counterparty says nothing — the ledger's own entries carry it too — so a gap
-- opened there is found only by the account checks below, never by entry.
-- After 060 a NULL counterparty is an entry that went past the Ledger.
--
-- Nothing here writes.

\pset title 'eras'
WITH era AS (
    SELECT (SELECT applied_at FROM schema_migrations WHERE version = '029_system_accounts.sql')        AS ledger_start,
           (SELECT applied_at FROM schema_migrations WHERE version = '060_transaction_counterparty.sql') AS counterparty_start
)
SELECT era.ledger_start::date       AS ledger_era_starts,
       era.counterparty_start::date AS counterparty_written_since,
       count(t.id) FILTER (WHERE t.created_at < era.ledger_start) AS before_ledger,
       count(t.id) FILTER (WHERE t.created_at >= era.ledger_start
                            AND (era.counterparty_start IS NULL OR t.created_at < era.counterparty_start)) AS unclassifiable,
       count(t.id) FILTER (WHERE t.created_at >= era.counterparty_start AND t.counterparty IS NOT NULL) AS two_sided,
       count(t.id) FILTER (WHERE t.created_at >= era.counterparty_start AND t.counterparty IS NULL)     AS one_sided,
       max(t.created_at)::date AS newest_entry
FROM era LEFT JOIN transactions t ON true
GROUP BY era.ledger_start, era.counterparty_start;

\pset title 'accounts against their own definitions'
SELECT 'ESCROW' AS account,
       (SELECT balance FROM system_accounts WHERE code='ESCROW') AS stored,
       (SELECT COALESCE(SUM(hold_amount),0) FROM orders WHERE status IN ('SEARCHING','ASSIGNED','EXECUTED')) AS should_be,
       (SELECT balance FROM system_accounts WHERE code='ESCROW')
         - (SELECT COALESCE(SUM(hold_amount),0) FROM orders WHERE status IN ('SEARCHING','ASSIGNED','EXECUTED')) AS drift
UNION ALL
SELECT 'FINES',
       (SELECT balance FROM system_accounts WHERE code='FINES'),
       (SELECT COALESCE(SUM(amount),0) FROM transactions WHERE type='FINE'),
       (SELECT balance FROM system_accounts WHERE code='FINES')
         - (SELECT COALESCE(SUM(amount),0) FROM transactions WHERE type='FINE')
UNION ALL
SELECT 'PAYOUTS',
       (SELECT balance FROM system_accounts WHERE code='PAYOUTS'),
       (SELECT COALESCE(SUM(amount),0) FROM balance_withdrawal_requests WHERE status='PENDING'),
       (SELECT balance FROM system_accounts WHERE code='PAYOUTS')
         - (SELECT COALESCE(SUM(amount),0) FROM balance_withdrawal_requests WHERE status='PENDING');

\pset title 'the gap itself'
SELECT (SELECT COALESCE(SUM(balance),0) FROM users)          AS users_hold,
       (SELECT COALESCE(SUM(balance),0) FROM system_accounts) AS accounts_hold,
       (SELECT COALESCE(SUM(balance),0) FROM users)
         + (SELECT COALESCE(SUM(balance),0) FROM system_accounts) AS gap;
