-- 023_pending_email_verification.sql

ALTER TABLE users ADD COLUMN IF NOT EXISTS pending_email VARCHAR(255) NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_token_expires_at TIMESTAMP WITH TIME ZONE NULL;
