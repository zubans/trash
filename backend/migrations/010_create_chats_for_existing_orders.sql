-- Create chat rooms for existing orders that do not have one yet.
INSERT INTO chats (order_id, is_active)
SELECT id, TRUE
FROM orders
WHERE id NOT IN (SELECT order_id FROM chats);
