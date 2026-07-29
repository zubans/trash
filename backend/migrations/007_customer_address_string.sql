-- 007_customer_address_string.sql

-- Store a single pickup address as plain text instead of JSONB.
ALTER TABLE customer_profiles ALTER COLUMN address TYPE VARCHAR;
ALTER TABLE customer_profiles ALTER COLUMN address SET DEFAULT '';

-- full_name remains optional for MVP; make it default to empty string.
ALTER TABLE customer_profiles ALTER COLUMN full_name SET DEFAULT '';

-- Allow empty full_name during registration.
ALTER TABLE customer_profiles ALTER COLUMN full_name DROP NOT NULL;
