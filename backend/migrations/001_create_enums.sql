-- 001_create_enums.sql
-- Create ENUM types for various columns

-- User role
CREATE TYPE role_type AS ENUM ('CUSTOMER', 'EXECUTOR', 'ADMIN');

-- User status
CREATE TYPE status_type AS ENUM ('ACTIVE', 'BANNED');

-- Order volume types
CREATE TYPE volume_type AS ENUM ('STANDARD', 'LARGE', 'CONSTRUCTION');

-- Speed tariff
CREATE TYPE speed_tariff_type AS ENUM ('REGULAR', 'URGENT', 'ASAP', 'CUSTOM');

-- Order status
CREATE TYPE order_status_type AS ENUM ('SEARCHING', 'ASSIGNED', 'COMPLETED', 'CANCELED');

-- Shift status
CREATE TYPE shift_status_type AS ENUM ('ACTIVE', 'COMPLETED', 'PENALIZED');

-- Transaction type
CREATE TYPE transaction_type AS ENUM ('TOP_UP', 'HOLD', 'PAYMENT', 'REWARD', 'FINE', 'REFUND');

-- Bid status
CREATE TYPE bid_status_type AS ENUM ('PENDING', 'ACCEPTED', 'REJECTED');

-- Note: Use UUID for primary keys. Ensure "uuid-ossp" extension is enabled if needed.
