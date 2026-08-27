-- 035_manual_user_verification.sql
-- Manual verification: an admin flips a checkbox to mark a user as verified.
--
-- Verification is deliberately a column of its own, kept apart from `status`
-- (ACTIVE/BANNED) and from `email_verified` (which only proves the user owns
-- their email address). The eligibility rules — "show orders only from verified
-- customers" and service variants with requires_verification — key off THIS
-- flag alone, so a user becomes verified only through an explicit admin action,
-- never as an automatic side effect of confirming their email.

ALTER TABLE users ADD COLUMN IF NOT EXISTS is_verified BOOLEAN NOT NULL DEFAULT false;

-- One-time continuity seed: users who were already treated as verified (they
-- had confirmed their email, which used to be the accidental verification
-- signal) stay verified so their existing orders keep flowing. From here on
-- verification is manual only. Drop this UPDATE if you want a clean slate where
-- every user starts unverified.
UPDATE users SET is_verified = true WHERE email_verified = true AND is_verified = false;
