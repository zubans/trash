-- 003_admin_finances.sql

-- Create ENUM type for topup status if it doesn't exist
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'topup_status') THEN
        CREATE TYPE topup_status AS ENUM ('PENDING', 'APPROVED', 'REJECTED');
    END IF;
END$$;

-- Create balance_topup_requests table
CREATE TABLE IF NOT EXISTS balance_topup_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    status topup_status NOT NULL DEFAULT 'PENDING',
    admin_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE
);

-- Create revoked_tokens table
CREATE TABLE IF NOT EXISTS revoked_tokens (
    token_hash VARCHAR PRIMARY KEY,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL
);

-- Seed default system settings
INSERT INTO system_settings (key, value) VALUES
('standard_tariff_coeff', 1.000000),
('increased_tariff_coeff', 2.000000),
('urgent_tariff_coeff', 3.000000),
('asap_tariff_coeff', 8.000000),
('fine_amount', 500.000000)
ON CONFLICT (key) DO NOTHING;

-- Seed default admin user
INSERT INTO users (id, role, phone, password, balance, status, created_at) VALUES
('00000000-0000-4000-a000-000000000000', 'ADMIN', '79999999999', '$2a$10$CkZNbgu5Bndaj18kPhdkouLjNvMgQXE.05W/xqH3Jj5JNvOrOET.i', 0, 'ACTIVE', now())
ON CONFLICT (phone) DO NOTHING;
