-- 008_rename_fine_amount.sql

-- Rename legacy fine_amount setting to the key used by the shift service.
UPDATE system_settings SET key = 'geofence_fine_amount' WHERE key = 'fine_amount';

-- Ensure default fine amount exists.
INSERT INTO system_settings (key, value) VALUES ('geofence_fine_amount', '500')
ON CONFLICT (key) DO NOTHING;
