-- Migration 030: saved pickup addresses.
--
-- The customer profile page offers up to two saved addresses, but the backend
-- only ever had one column, customer_profiles.address, and no endpoints to
-- manage a list. The page therefore showed "0/2 addresses" while the order form
-- happily used the address that was there.
--
-- This table becomes the source of truth for a customer's addresses. The
-- existing single address is copied in as the default one, so nobody loses what
-- they already had.

CREATE TABLE IF NOT EXISTS customer_addresses (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    address    TEXT NOT NULL,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_customer_addresses_user
    ON customer_addresses (user_id, created_at);

-- One default per customer.
CREATE UNIQUE INDEX IF NOT EXISTS idx_customer_addresses_one_default
    ON customer_addresses (user_id) WHERE is_default;

-- The same address twice in one account is a mistake, not a feature.
CREATE UNIQUE INDEX IF NOT EXISTS idx_customer_addresses_unique
    ON customer_addresses (user_id, address);

-- Carry over what customers already have.
INSERT INTO customer_addresses (user_id, address, is_default)
SELECT cp.user_id, cp.address, TRUE
FROM customer_profiles cp
WHERE cp.address IS NOT NULL
  AND btrim(cp.address::text, '"') <> ''
ON CONFLICT DO NOTHING;
