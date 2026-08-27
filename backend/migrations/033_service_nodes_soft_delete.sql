-- 033_service_nodes_soft_delete.sql
-- Soft delete for the service catalog.
--
-- orders.service_variant_id references service_nodes for the lifetime of the
-- order history, so a service that had ever been ordered could not be removed
-- at all: the repository refused the delete to keep that history readable.
-- Retiring a service now marks the row instead of removing it, which keeps
-- every historical order intact and makes deletion possible at any time.

ALTER TABLE service_nodes ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL;

-- A deleted node is never offered in the app: is_active is forced off on
-- delete, so every existing "is_active = TRUE" query already excludes it even
-- before the deleted_at filters below.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'chk_deleted_node_inactive'
    ) THEN
        ALTER TABLE service_nodes
            ADD CONSTRAINT chk_deleted_node_inactive
            CHECK (deleted_at IS NULL OR is_active = FALSE);
    END IF;
END $$;

-- code is unique among live nodes only, so a retired code can be used again by
-- a newly created service.
ALTER TABLE service_nodes DROP CONSTRAINT IF EXISTS service_nodes_code_key;
DROP INDEX IF EXISTS service_nodes_code_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_service_nodes_code_live
    ON service_nodes (code) WHERE deleted_at IS NULL;

-- The tree is walked parent by parent on every admin page load.
CREATE INDEX IF NOT EXISTS idx_service_nodes_parent_live
    ON service_nodes (parent_id) WHERE deleted_at IS NULL;

-- Deleting a service now asks "does this node have orders" for the response
-- message instead of as a hard guard; the lookup is by variant.
CREATE INDEX IF NOT EXISTS idx_orders_service_variant
    ON orders (service_variant_id);
