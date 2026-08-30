-- 040_service_node_moderator_only.sql
-- Moderator-only services: a service node marked with this flag produces orders
-- that are visible to and acceptable by moderators only. Modelled exactly like
-- requires_verification (a plain boolean toggled in the service constructor).

ALTER TABLE service_nodes ADD COLUMN IF NOT EXISTS moderator_only BOOLEAN NOT NULL DEFAULT false;
