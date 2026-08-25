-- Migration 025: consolidates the DDL that used to run from repository
-- constructors on every start, with the error discarded.
--
-- Every statement here is idempotent, so applying it to a database that the old
-- constructors had already patched is a no-op. Nothing new is introduced —
-- this migration only makes the existing schema reproducible.

-- users: email verification, password reset, personal data
ALTER TABLE users ADD COLUMN IF NOT EXISTS email VARCHAR(255) NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verification_token VARCHAR(255) NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_token_expires_at TIMESTAMP WITH TIME ZONE NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS pending_email VARCHAR(255) NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_reset_code VARCHAR(10) NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_reset_expires_at TIMESTAMP WITH TIME ZONE NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_reset_attempts INT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_name VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS first_name VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS patronymic VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS birth_date DATE NULL;

-- orders: free-form comment from the customer
ALTER TABLE orders ADD COLUMN IF NOT EXISTS comment TEXT NULL;

-- service catalog: executor restrictions
ALTER TABLE service_nodes ADD COLUMN IF NOT EXISTS requires_verification BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE service_nodes ADD COLUMN IF NOT EXISTS min_age INT NOT NULL DEFAULT 0;

-- chat messages: edit timestamp
ALTER TABLE messages ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NULL;

-- withdrawal requests
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'withdrawal_status') THEN
        CREATE TYPE withdrawal_status AS ENUM ('PENDING', 'APPROVED', 'REJECTED');
    END IF;
END$$;

ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'WITHDRAWAL';

CREATE TABLE IF NOT EXISTS balance_withdrawal_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    status withdrawal_status NOT NULL DEFAULT 'PENDING',
    admin_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE
);

-- support chats
CREATE TABLE IF NOT EXISTS support_chats (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    is_banned BOOLEAN NOT NULL DEFAULT FALSE,
    banned_until TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE support_chats ADD COLUMN IF NOT EXISTS is_banned BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE support_chats ADD COLUMN IF NOT EXISTS banned_until TIMESTAMPTZ NULL;

CREATE TABLE IF NOT EXISTS support_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_id UUID NOT NULL REFERENCES support_chats(id) ON DELETE CASCADE,
    sender_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    text TEXT NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'sent',
    file_url TEXT NULL,
    file_name TEXT NULL,
    file_type VARCHAR(32) NULL,
    file_size BIGINT NULL,
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NULL
);
