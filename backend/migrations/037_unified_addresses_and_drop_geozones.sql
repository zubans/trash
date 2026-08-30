-- 037_unified_addresses_and_drop_geozones.sql
-- Create unified addresses table for both customers and executors.
-- Drop legacy address fields and obsolete geozones.

CREATE TABLE IF NOT EXISTS addresses (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    address    TEXT NOT NULL,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    region     TEXT,
    city       TEXT,
    street     TEXT,
    house      TEXT,
    flat       TEXT,
    fias_id    TEXT,
    geo_lat    DOUBLE PRECISION,
    geo_lon    DOUBLE PRECISION,
    source     TEXT NOT NULL DEFAULT 'dadata',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_addresses_user
    ON addresses (user_id, created_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_addresses_one_default
    ON addresses (user_id) WHERE is_default;

CREATE UNIQUE INDEX IF NOT EXISTS idx_addresses_unique
    ON addresses (user_id, address);

-- Clean up legacy customer_addresses table
DROP TABLE IF EXISTS customer_addresses CASCADE;

-- Clean up obsolete geozones and related logs/constraints
DROP TABLE IF EXISTS shift_gps_logs CASCADE;
ALTER TABLE IF EXISTS executor_profiles DROP CONSTRAINT IF EXISTS fk_executor_geozone;
ALTER TABLE IF EXISTS executor_profiles DROP COLUMN IF EXISTS work_area_id;
DROP TABLE IF EXISTS geozones CASCADE;
DROP TYPE IF EXISTS geozone_type CASCADE;

-- Remove redundant/duplicate address columns from customer_profiles
ALTER TABLE IF EXISTS customer_profiles
    DROP COLUMN IF EXISTS address,
    DROP COLUMN IF EXISTS address_flat,
    DROP COLUMN IF EXISTS address_fias_id,
    DROP COLUMN IF EXISTS address_lat,
    DROP COLUMN IF EXISTS address_lon,
    DROP COLUMN IF EXISTS last_geo;
