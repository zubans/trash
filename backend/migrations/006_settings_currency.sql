-- 006_settings_currency.sql

-- Convert system_settings.value to VARCHAR so it can store both numbers and strings.
ALTER TABLE system_settings ALTER COLUMN value TYPE VARCHAR;

-- Seed default currency (RUB).
INSERT INTO system_settings (key, value) VALUES ('currency', 'RUB')
ON CONFLICT (key) DO NOTHING;
