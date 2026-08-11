-- 014_executor_location_interval.sql
-- Default interval (seconds) for automatic executor GPS location submission.
INSERT INTO system_settings (key, value) VALUES ('executor_location_send_interval_seconds', '5')
ON CONFLICT (key) DO NOTHING;
