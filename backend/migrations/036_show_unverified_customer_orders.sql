-- 036_show_unverified_customer_orders.sql
-- Toggle: whether executors see orders from customers who have NOT been
-- manually verified (users.is_verified = false).
--
-- '0' (default) keeps the stricter rule — the executor order list and the
-- executor map both hide unverified customers' orders. '1' shows every
-- customer's orders on both surfaces. Kept as a "0"/"1" flag, like
-- geofence_tracking_enabled, so it cannot be switched on by a typo.
INSERT INTO system_settings (key, value) VALUES ('show_unverified_customer_orders', '0')
ON CONFLICT (key) DO NOTHING;
