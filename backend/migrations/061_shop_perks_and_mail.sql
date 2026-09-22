-- 061_shop_perks_and_mail.sql
-- What the shop needs beyond migrations 058 and 059.
--
-- Two things user_perks did not record.
--
-- A perk granted by hand (compensation, promotion) has no purchase behind it,
-- so shop_order_id says nothing about who gave it or why. granted_by and
-- reason answer that on the user card, the way revoked_by answers it for a
-- revocation.
--
-- reminded_at marks that the "your perk ends in three days" letter went out.
-- The worker runs hourly and on one leader, but a mark in the row, set in the
-- same statement that claims it, is what keeps the letter to one.

ALTER TABLE user_perks ADD COLUMN IF NOT EXISTS granted_by UUID NULL REFERENCES users(id);
ALTER TABLE user_perks ADD COLUMN IF NOT EXISTS reason TEXT NULL;
ALTER TABLE user_perks ADD COLUMN IF NOT EXISTS reminded_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_user_perks_shop_order
    ON user_perks (shop_order_id) WHERE shop_order_id IS NOT NULL;

-- Shop letters: payment, status changes, cancellation, a granted perk and the
-- reminder that it ends. A kind of their own, so the app can open the
-- purchase instead of parsing the text.
ALTER TABLE user_mail DROP CONSTRAINT IF EXISTS user_mail_kind_check;
ALTER TABLE user_mail ADD CONSTRAINT user_mail_kind_check
    CHECK (kind IN ('ACHIEVEMENT', 'GIFT', 'PROMO', 'NEWS', 'SYSTEM', 'DIRECT', 'SHOP'));
