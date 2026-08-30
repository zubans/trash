-- +migrate no-transaction
-- 038_order_commission.sql
-- Platform commission on completed orders.
--
-- This migration must run OUTSIDE a transaction: PostgreSQL refuses
-- "ALTER TYPE ... ADD VALUE" inside a transaction block, so wrapping it (the
-- default) makes the whole migration fail, which in turn blocks every later
-- migration (e.g. 039 user_roles) from applying. Every statement below is
-- individually idempotent (IF NOT EXISTS / ON CONFLICT), so running without the
-- wrapping transaction is safe to re-run.
--
-- A completed order used to drain escrow entirely to the executor. Now a
-- configurable share of the final amount is kept by the platform, and it has to
-- land somewhere the books can see: a system account of its own, so the
-- collected commission is a balance that can be read and paid out rather than a
-- number derived from the order log.
--
-- Two transaction types carry the two halves, both with sign 0 because neither
-- moves a user balance:
--
--   COMMISSION         escrow -> COMMISSION when an order is confirmed
--   COMMISSION_PAYOUT  COMMISSION -> DEPOSITS when an admin withdraws it
--
-- The customer still pays exactly the final amount and escrow still drains to
-- zero on confirmation, so no escrow drift is introduced: the executor's reward
-- is simply smaller by the commission.

ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'COMMISSION';
ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'COMMISSION_PAYOUT';

INSERT INTO system_accounts (code, name) VALUES
    ('COMMISSION', 'Комиссия платформы')
ON CONFLICT (code) DO NOTHING;

-- Percent of the final order amount kept by the platform. Starts at 0 so
-- deploying this migration changes nobody's payout until an admin sets a rate.
INSERT INTO system_settings (key, value) VALUES ('order_commission_percent', '0')
ON CONFLICT (key) DO NOTHING;
