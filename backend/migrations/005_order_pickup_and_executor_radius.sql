-- 005_order_pickup_and_executor_radius.sql

-- Add pickup coordinates and address to orders
ALTER TABLE orders
ADD COLUMN IF NOT EXISTS address VARCHAR,
ADD COLUMN IF NOT EXISTS pickup_lat NUMERIC(9,6),
ADD COLUMN IF NOT EXISTS pickup_lon NUMERIC(9,6);

-- Add search radius to executor profiles (default 2000 meters = 2 km)
ALTER TABLE executor_profiles
ADD COLUMN IF NOT EXISTS search_radius_meters INT NOT NULL DEFAULT 2000;

-- Indexes for radius search
CREATE INDEX IF NOT EXISTS idx_orders_pickup_coords ON orders(pickup_lat, pickup_lon)
WHERE pickup_lat IS NOT NULL AND pickup_lon IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
