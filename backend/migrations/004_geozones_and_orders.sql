-- 004_geozones_and_orders.sql

-- Create ENUM type for geozone type if it doesn't exist
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'geozone_type') THEN
        CREATE TYPE geozone_type AS ENUM ('CIRCLE', 'POLYGON');
    END IF;
END$$;

-- Create geozones table
CREATE TABLE IF NOT EXISTS geozones (
    id SERIAL PRIMARY KEY,
    name VARCHAR NOT NULL,
    type geozone_type NOT NULL,
    center_latitude NUMERIC(9,6),
    center_longitude NUMERIC(9,6),
    radius_meters NUMERIC(18,2),
    coordinates JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

-- Add constraint to executor_profiles
ALTER TABLE executor_profiles 
ADD CONSTRAINT fk_executor_geozone 
FOREIGN KEY (work_area_id) 
REFERENCES geozones(id) 
ON DELETE SET NULL;

-- Add fine_amount to shifts
ALTER TABLE shifts ADD COLUMN IF NOT EXISTS fine_amount NUMERIC(18,2) NOT NULL DEFAULT 0;

-- Create shift_gps_logs table
CREATE TABLE IF NOT EXISTS shift_gps_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    shift_id UUID NOT NULL REFERENCES shifts(id) ON DELETE CASCADE,
    latitude NUMERIC(9,6) NOT NULL,
    longitude NUMERIC(9,6) NOT NULL,
    is_inside BOOLEAN NOT NULL DEFAULT TRUE,
    recorded_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

-- Index for performance
CREATE INDEX IF NOT EXISTS idx_gps_logs_shift ON shift_gps_logs(shift_id);

-- Seed default Geozones
INSERT INTO geozones (id, name, type, center_latitude, center_longitude, radius_meters, coordinates) VALUES
(1, 'Moscow Center (Circle)', 'CIRCLE', 55.755800, 37.617300, 5000.00, NULL),
(2, 'Moscow Suburbs (Polygon)', 'POLYGON', NULL, NULL, NULL, '[[55.850000, 37.500000], [55.850000, 37.700000], [55.650000, 37.700000], [55.650000, 37.500000]]'::jsonb)
ON CONFLICT (id) DO NOTHING;

-- Adjust SERIAL sequence for geozones id
SELECT setval('geozones_id_seq', (SELECT MAX(id) FROM geozones));

-- Seed default Executor user (password is "admin")
INSERT INTO users (id, role, phone, password, balance, status, created_at) VALUES
('11111111-1111-4111-a111-111111111111', 'EXECUTOR', '79991111111', '$2a$10$CkZNbgu5Bndaj18kPhdkouLjNvMgQXE.05W/xqH3Jj5JNvOrOET.i', 0, 'ACTIVE', now())
ON CONFLICT (phone) DO NOTHING;

-- Seed default Executor profile
INSERT INTO executor_profiles (user_id, full_name, work_area_id) VALUES
('11111111-1111-4111-a111-111111111111', 'Ivan Shiftov', 1)
ON CONFLICT (user_id) DO NOTHING;
