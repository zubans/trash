-- Migration 031: keep addresses as their parts, not as one line.
--
-- Until now an address was a single string, and both the client and the server
-- recovered its components with the same regular expression:
--
--     ^Россия,\s*([^,]+?),\s*([^,]+?),\s*д\.\s*(\d+)(?:\s+кв\.\s*(\d+))?$
--
-- That accepted only a purely numeric house number, so 12к1, 10 стр. 2 and 5А —
-- ordinary Russian addresses — were rejected after the user had already picked
-- them from the suggestion list. It also meant the apartment lived inside the
-- string, and coordinates lived nowhere at all for addresses added from the
-- profile page, which left those addresses invisible to distance matching.
--
-- The parts are now stored as they arrive from the address provider, together
-- with the register identifier and the coordinates. The `address` column stays
-- as the display line so that nothing which reads it has to change at once.

ALTER TABLE customer_addresses ADD COLUMN IF NOT EXISTS region   TEXT;
ALTER TABLE customer_addresses ADD COLUMN IF NOT EXISTS city     TEXT;
ALTER TABLE customer_addresses ADD COLUMN IF NOT EXISTS street   TEXT;
ALTER TABLE customer_addresses ADD COLUMN IF NOT EXISTS house    TEXT;
ALTER TABLE customer_addresses ADD COLUMN IF NOT EXISTS flat     TEXT;
ALTER TABLE customer_addresses ADD COLUMN IF NOT EXISTS fias_id  TEXT;
ALTER TABLE customer_addresses ADD COLUMN IF NOT EXISTS geo_lat  DOUBLE PRECISION;
ALTER TABLE customer_addresses ADD COLUMN IF NOT EXISTS geo_lon  DOUBLE PRECISION;

-- Which provider produced the address, so a support question about a wrong
-- address can be traced. 'legacy' marks the rows backfilled just below.
ALTER TABLE customer_addresses ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'legacy';

-- Looking an address up by its register identifier is how a saved address is
-- recognised as the same place as a newly suggested one.
CREATE INDEX IF NOT EXISTS idx_customer_addresses_fias
    ON customer_addresses (fias_id) WHERE fias_id IS NOT NULL;

-- Backfill. Every address already stored had to satisfy the regular expression
-- above to be saved at all, so the shape is known exactly and the split is
-- safe. The house pattern is deliberately looser than the one that created
-- these rows, so that anything hand-edited in the database still splits.
UPDATE customer_addresses AS ca
SET city   = COALESCE(NULLIF(btrim(m[1]), ''), ca.city),
    street = COALESCE(NULLIF(btrim(m[2]), ''), ca.street),
    house  = COALESCE(NULLIF(btrim(m[3]), ''), ca.house),
    flat   = COALESCE(NULLIF(btrim(m[4]), ''), ca.flat)
FROM (
    SELECT id,
           regexp_match(
               address,
               '^Россия,\s*([^,]+?),\s*([^,]+?),\s*д\.\s*([^,]+?)(?:[\s,]+кв\.\s*(.+?))?\s*$'
           ) AS m
    FROM customer_addresses
) AS parsed
WHERE ca.id = parsed.id
  AND parsed.m IS NOT NULL
  AND ca.city IS NULL;

-- The same address is carried on the customer profile, which is what the older
-- clients still read and write.
ALTER TABLE customer_profiles ADD COLUMN IF NOT EXISTS address_flat    TEXT;
ALTER TABLE customer_profiles ADD COLUMN IF NOT EXISTS address_fias_id TEXT;
ALTER TABLE customer_profiles ADD COLUMN IF NOT EXISTS address_lat     DOUBLE PRECISION;
ALTER TABLE customer_profiles ADD COLUMN IF NOT EXISTS address_lon     DOUBLE PRECISION;
