-- 019_add_executed_to_order_status.sql
-- Add EXECUTED value to order_status_type ENUM if not already present

ALTER TYPE order_status_type ADD VALUE IF NOT EXISTS 'EXECUTED' AFTER 'ASSIGNED';
