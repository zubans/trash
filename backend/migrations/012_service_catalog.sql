-- 012_service_catalog.sql
-- Service catalog with hierarchical structure and mobile app releases.
-- This migration is idempotent: it uses IF NOT EXISTS and checks column
-- existence before altering the orders table.

CREATE TABLE IF NOT EXISTS service_nodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_id UUID REFERENCES service_nodes(id),
    code VARCHAR UNIQUE NOT NULL,
    name JSONB NOT NULL,
    description JSONB,
    node_type VARCHAR NOT NULL CHECK (node_type IN ('CATEGORY', 'VARIANT')),
    base_price NUMERIC(18,2),
    is_auction BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT chk_variant_has_price
        CHECK (node_type != 'VARIANT' OR base_price IS NOT NULL),
    CONSTRAINT chk_category_no_price
        CHECK (node_type != 'CATEGORY' OR base_price IS NULL)
);

CREATE TABLE IF NOT EXISTS service_node_paths (
    ancestor_id UUID REFERENCES service_nodes(id) ON DELETE CASCADE,
    descendant_id UUID REFERENCES service_nodes(id) ON DELETE CASCADE,
    depth INT NOT NULL,
    PRIMARY KEY (ancestor_id, descendant_id)
);

CREATE INDEX IF NOT EXISTS idx_service_node_paths_ancestor ON service_node_paths(ancestor_id);
CREATE INDEX IF NOT EXISTS idx_service_node_paths_descendant ON service_node_paths(descendant_id);

CREATE TABLE IF NOT EXISTS mobile_app_releases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    platform VARCHAR NOT NULL CHECK (platform IN ('android')),
    version_name VARCHAR NOT NULL,
    version_code INT NOT NULL,
    file_name VARCHAR NOT NULL,
    file_path VARCHAR NOT NULL,
    release_notes TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    force_update BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_mobile_app_releases_active
    ON mobile_app_releases(platform, version_name)
    WHERE is_active = TRUE;

CREATE OR REPLACE FUNCTION rebuild_service_node_paths(p_node_id UUID)
RETURNS VOID AS $$
BEGIN
    -- Collect the node and all of its descendants into a temporary table.
    CREATE TEMP TABLE IF NOT EXISTS tmp_rebuild_descendants (
        id UUID PRIMARY KEY,
        depth INT NOT NULL
    ) ON COMMIT DROP;
    TRUNCATE tmp_rebuild_descendants;

    WITH RECURSIVE descendants AS (
        SELECT id, parent_id, 1 AS depth
        FROM service_nodes
        WHERE parent_id = p_node_id
        UNION ALL
        SELECT sn.id, sn.parent_id, d.depth + 1
        FROM service_nodes sn
        JOIN descendants d ON sn.parent_id = d.id
    )
    INSERT INTO tmp_rebuild_descendants (id, depth)
    SELECT id, depth FROM descendants;

    -- 1. Remove old paths for the node and its subtree.
    DELETE FROM service_node_paths
    WHERE descendant_id IN (
        SELECT id FROM service_nodes WHERE id = p_node_id
        UNION ALL
        SELECT id FROM tmp_rebuild_descendants
    );

    -- 2. Self-path (depth 0).
    INSERT INTO service_node_paths (ancestor_id, descendant_id, depth)
    VALUES (p_node_id, p_node_id, 0);

    -- 3. Paths from all ancestors to the node itself.
    INSERT INTO service_node_paths (ancestor_id, descendant_id, depth)
    SELECT sp.ancestor_id, p_node_id, sp.depth + 1
    FROM service_node_paths sp
    WHERE sp.descendant_id = (SELECT parent_id FROM service_nodes WHERE id = p_node_id);

    -- 4. Paths from the node to all its descendants.
    INSERT INTO service_node_paths (ancestor_id, descendant_id, depth)
    SELECT p_node_id, id, depth
    FROM tmp_rebuild_descendants;

    -- 5. Paths from ancestors (except the node itself) to all descendants.
    INSERT INTO service_node_paths (ancestor_id, descendant_id, depth)
    SELECT a.ancestor_id, d.id, a.depth + d.depth
    FROM tmp_rebuild_descendants d
    JOIN (
        SELECT ancestor_id, depth
        FROM service_node_paths
        WHERE descendant_id = p_node_id AND ancestor_id <> p_node_id
    ) a ON TRUE;
END;
$$ LANGUAGE plpgsql;

-- Seed data: fixed UUIDs so that the orders data migration is deterministic.
INSERT INTO service_nodes (id, parent_id, code, name, description, node_type, base_price, is_auction, is_active, sort_order) VALUES
('11111111-1111-1111-1111-111111111111', NULL, 'trash_removal',
 '{"ru": "Вынос мусора", "en": "Trash removal"}',
 '{"ru": "Вывоз бытовых отходов", "en": "Household waste removal"}',
 'CATEGORY', NULL, FALSE, TRUE, 1),
('22222222-2222-2222-2222-222222222222', '11111111-1111-1111-1111-111111111111', 'trash_standard',
 '{"ru": "Стандартный", "en": "Standard"}',
 '{"ru": "Стандартный объём", "en": "Standard volume"}',
 'CATEGORY', NULL, FALSE, TRUE, 1),
('33333333-3333-3333-3333-333333333333', '22222222-2222-2222-2222-222222222222', 'trash_standard_single',
 '{"ru": "Один пакет", "en": "Single bag"}',
 '{"ru": "До 30 литров", "en": "Up to 30 liters"}',
 'VARIANT', 100.00, FALSE, TRUE, 1),
('44444444-4444-4444-4444-444444444444', '22222222-2222-2222-2222-222222222222', 'trash_standard_double',
 '{"ru": "Двойной пакет", "en": "Double bag"}',
 '{"ru": "До 60 литров", "en": "Up to 60 liters"}',
 'VARIANT', 180.00, FALSE, TRUE, 2),
('55555555-5555-5555-5555-555555555555', '11111111-1111-1111-1111-111111111111', 'trash_large',
 '{"ru": "Большой", "en": "Large"}',
 '{"ru": "Крупногабаритный мусор", "en": "Bulky waste"}',
 'CATEGORY', NULL, FALSE, TRUE, 2),
('66666666-6666-6666-6666-666666666666', '55555555-5555-5555-5555-555555555555', 'trash_large_regular',
 '{"ru": "Большой обычный", "en": "Large regular"}',
 NULL, 'VARIANT', 200.00, FALSE, TRUE, 1),
('77777777-7777-7777-7777-777777777777', '11111111-1111-1111-1111-111111111111', 'trash_construction',
 '{"ru": "Строительный мусор", "en": "Construction waste"}',
 '{"ru": "Аукцион исполнителей", "en": "Executor auction"}',
 'VARIANT', 0.00, TRUE, TRUE, 3),
('88888888-8888-8888-8888-888888888888', NULL, 'dog_walking',
 '{"ru": "Выгул собак", "en": "Dog walking"}',
 NULL, 'CATEGORY', NULL, FALSE, TRUE, 2),
('99999999-9999-9999-9999-999999999999', '88888888-8888-8888-8888-888888888888', 'dog_walk_morning',
 '{"ru": "Утренний выгул", "en": "Morning walk"}',
 NULL, 'VARIANT', 150.00, FALSE, TRUE, 1),
('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', '88888888-8888-8888-8888-888888888888', 'dog_walk_evening',
 '{"ru": "Вечерний выгул", "en": "Evening walk"}',
 NULL, 'VARIANT', 150.00, FALSE, TRUE, 2),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', '88888888-8888-8888-8888-888888888888', 'dog_overnight',
 '{"ru": "Передержка", "en": "Overnight stay"}',
 NULL, 'VARIANT', 800.00, FALSE, TRUE, 3)
ON CONFLICT (id) DO UPDATE SET
    parent_id = EXCLUDED.parent_id,
    code = EXCLUDED.code,
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    node_type = EXCLUDED.node_type,
    base_price = EXCLUDED.base_price,
    is_auction = EXCLUDED.is_auction,
    is_active = EXCLUDED.is_active,
    sort_order = EXCLUDED.sort_order;

-- Build closure table paths. Order matters: roots before descendants.
SELECT rebuild_service_node_paths('11111111-1111-1111-1111-111111111111');
SELECT rebuild_service_node_paths('22222222-2222-2222-2222-222222222222');
SELECT rebuild_service_node_paths('55555555-5555-5555-5555-555555555555');
SELECT rebuild_service_node_paths('88888888-8888-8888-8888-888888888888');
SELECT rebuild_service_node_paths('33333333-3333-3333-3333-333333333333');
SELECT rebuild_service_node_paths('44444444-4444-4444-4444-444444444444');
SELECT rebuild_service_node_paths('66666666-6666-6666-6666-666666666666');
SELECT rebuild_service_node_paths('77777777-7777-7777-7777-777777777777');
SELECT rebuild_service_node_paths('99999999-9999-9999-9999-999999999999');
SELECT rebuild_service_node_paths('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa');
SELECT rebuild_service_node_paths('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb');

-- Add new order columns only if they do not exist yet.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'orders' AND column_name = 'service_variant_id'
    ) THEN
        ALTER TABLE orders ADD COLUMN service_variant_id UUID REFERENCES service_nodes(id);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'orders' AND column_name = 'is_urgent'
    ) THEN
        ALTER TABLE orders ADD COLUMN is_urgent BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'orders' AND column_name = 'is_asap'
    ) THEN
        ALTER TABLE orders ADD COLUMN is_asap BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;

-- Migrate legacy data only when the old columns still exist.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'orders' AND column_name = 'volume_type'
    ) THEN
        UPDATE orders
        SET
            service_variant_id = CASE
                WHEN volume_type = 'STANDARD' THEN '33333333-3333-3333-3333-333333333333'::UUID
                WHEN volume_type = 'LARGE' THEN '66666666-6666-6666-6666-666666666666'::UUID
                WHEN volume_type = 'CONSTRUCTION' THEN '77777777-7777-7777-7777-777777777777'::UUID
            END,
            is_urgent = CASE WHEN speed_tariff = 'URGENT' THEN TRUE ELSE FALSE END,
            is_asap = CASE WHEN speed_tariff = 'ASAP' THEN TRUE ELSE FALSE END
        WHERE service_variant_id IS NULL;

        IF EXISTS (SELECT 1 FROM orders WHERE service_variant_id IS NULL) THEN
            RAISE EXCEPTION 'Found orders without service_variant_id mapping';
        END IF;

        ALTER TABLE orders ALTER COLUMN service_variant_id SET NOT NULL;
        ALTER TABLE orders DROP COLUMN volume_type;
        ALTER TABLE orders DROP COLUMN speed_tariff;
    END IF;
END $$;

-- Drop the old enum type if it still exists and is unused.
DROP TYPE IF EXISTS volume_type;

-- speed_tariff_type may still be referenced by other objects; drop manually after verification.
-- DROP TYPE IF EXISTS speed_tariff_type;

-- Ensure tariff coefficients exist for the new pricing model.
INSERT INTO system_settings (key, value)
VALUES
    ('urgent_tariff_coeff', '3.0'),
    ('asap_tariff_coeff', '8.0')
ON CONFLICT (key) DO NOTHING;
