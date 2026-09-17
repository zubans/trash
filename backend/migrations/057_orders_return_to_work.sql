-- 057_orders_return_to_work.sql
-- Returning an order that is on review (EXECUTED, the executor marked it done,
-- the customer has not confirmed) back to work is an edit of the orders
-- section: orders.edit. The moderator already resolves checks and disputes,
-- and a mark set without the work being done is the same kind of case, so the
-- moderator gets the orders section together with the right to act in it.
-- ADMIN needs no rows: it passes every check in code.
INSERT INTO role_permissions (role_code, permission) VALUES
    ('MODERATOR', 'orders.view'),
    ('MODERATOR', 'orders.edit')
ON CONFLICT DO NOTHING;
