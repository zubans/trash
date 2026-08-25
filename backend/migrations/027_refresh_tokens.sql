-- Migration 027: refresh tokens.
--
-- Access tokens stay short-lived (15 minutes). A refresh token is an opaque
-- random string; only its SHA-256 hash is stored, so a database leak does not
-- hand out usable sessions.
--
-- Rotation is enforced by used_at: every refresh consumes one row and issues a
-- new one. A token presented twice means the value leaked, and the whole family
-- (all sessions of that user) is revoked — see AuthService.Refresh.

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  CHAR(64) NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    used_at     TIMESTAMPTZ NULL,
    revoked_at  TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens (user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires ON refresh_tokens (expires_at);
