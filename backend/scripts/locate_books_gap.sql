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
-- The era boundary is printed first because it decides whether the rest can be
-- trusted at all: with no counterparty ever recorded there is no ledger era to
-- reason about.
--
-- Nothing here writes.

SELECT count(*) FILTER (WHERE counterparty IS NOT NULL) AS with_counterparty,
       count(*) FILTER (WHERE counterparty IS NULL)     AS without_counterparty,
       min(created_at) FILTER (WHERE counterparty IS NOT NULL)::date AS ledger_era_starts,
       max(created_at)::date AS newest_entry
FROM transactions;

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
