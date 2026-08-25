-- Migration 028: withdrawal requests reserve the money when they are created.
--
-- A request used to only check the balance and leave the money spendable, so a
-- user could queue several requests against the same funds and keep spending it
-- meanwhile. The funds are now held at request time, which needs two ledger
-- types mirroring the order flow (HOLD then PAYMENT):
--
--   WITHDRAWAL_HOLD  the money leaves the balance when the request is created
--   WITHDRAWAL_PAID  the held money is actually paid out on approval
--
-- The historical WITHDRAWAL type keeps its meaning (a debit at approval time) so
-- that reconciliation of existing rows stays correct.

ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'WITHDRAWAL_HOLD';
ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'WITHDRAWAL_PAID';
