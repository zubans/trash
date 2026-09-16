-- 056_order_executed_at.sql
-- When the executor marked the order executed, by the server's clock.
--
-- Arbitration compares a photo's time with the "executed" mark, and the order
-- kept no such moment: the status changed, the time was lost. 054 added the
-- device's own time of the mark (executed_at_device); this is the server's side
-- of the same event. The two differ when the mark waited in the offline queue,
-- and arbitration shows both.
--
-- Orders marked executed before this migration keep NULL: the moment is gone,
-- and inventing it from another column would present a guess as evidence.

ALTER TABLE orders ADD COLUMN IF NOT EXISTS executed_at TIMESTAMPTZ NULL;
