-- Migration 016: Add file attachment support to chat messages
ALTER TABLE messages ADD COLUMN IF NOT EXISTS file_url TEXT NULL;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS file_name TEXT NULL;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS file_type VARCHAR(50) NULL;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS file_size BIGINT NULL;
