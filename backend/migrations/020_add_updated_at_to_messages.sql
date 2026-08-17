-- Migration 020: Add updated_at column to messages table for tracking edited chat messages
ALTER TABLE messages ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NULL;
