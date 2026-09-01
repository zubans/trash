-- 044_service_node_scripts.sql
-- A special service carries its own script, written in the admin panel.
--
-- Migration 043 introduced behaviours as files that ship with the build. That
-- covers the behaviours we write; it does not cover the ones an operator needs
-- to add or correct without a release. A node may now hold its own pair of
-- scripts, edited in the service constructor:
--
--   behavior_constants  config.star — the constants and everything the script
--                       exchanges with the core (amounts, roles, event names)
--   behavior_source     behavior.star — MANIFEST and the hooks
--
-- Precedence: a node with its own source runs it; a node with only a
-- behavior_code runs the file behaviour of that name, which is also what the
-- editor offers as the starting template. Neither column is validated here —
-- the server compiles the pair before it accepts the save, and refuses a node
-- whose script does not compile.

ALTER TABLE service_nodes ADD COLUMN IF NOT EXISTS behavior_constants TEXT;
ALTER TABLE service_nodes ADD COLUMN IF NOT EXISTS behavior_source TEXT;

-- Every node whose script has to be compiled at startup, in one query.
CREATE INDEX IF NOT EXISTS idx_service_nodes_with_script
    ON service_nodes (id)
    WHERE behavior_source IS NOT NULL AND deleted_at IS NULL;
