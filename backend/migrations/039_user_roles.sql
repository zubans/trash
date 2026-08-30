-- 039_user_roles.sql
-- Multi-role support: a user may hold several roles at once (e.g. EXECUTOR and
-- MODERATOR). Authorization keys off the set in this table; users.role stays as
-- the "primary/active" role that drives the default dashboard and keeps older
-- code paths working.
--
-- The MODERATOR role is introduced here as a valid value alongside
-- CUSTOMER / EXECUTOR / ADMIN. Role is stored as free text (matching users.role),
-- so no enum change is needed.

CREATE TABLE IF NOT EXISTS user_roles (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role    text NOT NULL,
    PRIMARY KEY (user_id, role)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_user ON user_roles(user_id);

-- Continuity seed: every existing user keeps the role they already have.
INSERT INTO user_roles (user_id, role)
SELECT id, role FROM users
ON CONFLICT (user_id, role) DO NOTHING;
