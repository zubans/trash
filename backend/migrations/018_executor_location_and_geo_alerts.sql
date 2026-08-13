-- 018_executor_location_and_geo_alerts.sql

ALTER TABLE executor_profiles
    ADD COLUMN IF NOT EXISTS current_lat NUMERIC(18, 8),
    ADD COLUMN IF NOT EXISTS current_lon NUMERIC(18, 8),
    ADD COLUMN IF NOT EXISTS last_manual_location_change_at TIMESTAMP WITH TIME ZONE;

CREATE TABLE IF NOT EXISTS geo_alerts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    executor_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    old_lat NUMERIC(18, 8),
    old_lon NUMERIC(18, 8),
    new_lat NUMERIC(18, 8) NOT NULL,
    new_lon NUMERIC(18, 8) NOT NULL,
    calculated_speed_kmh NUMERIC(10, 2) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_geo_alerts_executor ON geo_alerts(executor_id);
CREATE INDEX IF NOT EXISTS idx_geo_alerts_status ON geo_alerts(status);
