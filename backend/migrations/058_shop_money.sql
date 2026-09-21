-- +migrate no-transaction
-- 058_shop_money.sql
-- Money side of the shop: its own system account and its transaction types.
--
-- This migration must run OUTSIDE a transaction: PostgreSQL refuses
-- "ALTER TYPE ... ADD VALUE" inside a transaction block (see
-- 038_order_commission.sql, which introduced the pattern). Every statement is
-- individually idempotent (IF NOT EXISTS / ON CONFLICT), so a re-run is safe.
--
-- The shop sells goods (perks, merch, certificates) for money from the user's
-- internal balance. The revenue has to land somewhere the books can see: a
-- system account of its own, like COMMISSION for order commission, so the
-- collected revenue is a balance that can be read, refunded from and paid out
-- rather than a number derived from the order log.
--
-- Three transaction types carry the three movements:
--
--   SHOP_PURCHASE  user -> SHOP, sign -1: paying for a purchase
--   SHOP_REFUND    SHOP -> user, sign +1: refunding a canceled purchase; SHOP
--                  may go negative when revenue was already paid out — the
--                  refund is the platform's obligation, not a function of the
--                  account balance
--   SHOP_PAYOUT    SHOP -> DEPOSITS, sign 0: an admin withdrew the revenue;
--                  it moves between two system accounts and touches no user
--
-- The tables that use these types arrive in 059_shop.sql; separating the enum
-- change from the DDL keeps the enum values committed before any code can
-- reference them and keeps the table creation inside a transaction.

ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'SHOP_PURCHASE';
ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'SHOP_REFUND';
ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'SHOP_PAYOUT';

INSERT INTO system_accounts (code, name) VALUES ('SHOP', 'Выручка магазина')
ON CONFLICT (code) DO NOTHING;
