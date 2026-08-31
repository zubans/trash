-- 042_perf_indexes.sql
-- Indexes for the queries that run on every client poll.
--
-- These are plain (non-CONCURRENT) index builds, which take a write lock on the
-- table for the duration. That is deliberate: the runner applies a whole
-- migration file in one Exec, and PostgreSQL wraps a multi-statement simple
-- query in an implicit transaction, which CREATE INDEX CONCURRENTLY refuses to
-- run inside — the marker "-- +migrate no-transaction" does not help, because it
-- only skips the runner's own BEGIN, not the implicit one. Splitting this into
-- one file per index would allow CONCURRENTLY; at the current table sizes these
-- builds take milliseconds, so the lock is not worth five files. Revisit if
-- these tables grow to where an exclusive lock during deploy is a real outage.

-- CountActiveOrdersByExecutor and GetExecutorAssignedOrders both filter on the
-- pair. idx_orders_executor covers only executor_id, so the status filter is a
-- heap re-check on every matched row — and the matching worker calls the count
-- once per candidate executor per cycle.
CREATE INDEX IF NOT EXISTS idx_orders_executor_status
    ON orders (executor_id, status) WHERE executor_id IS NOT NULL;

-- FindNearbyOrders filters status and a coordinate bounding box together. The
-- existing indexes split that: one on status, one on the coordinates. The
-- planner picks one and re-checks the rest, which on a large orders table means
-- reading every searching order in the country to answer "what is near me".
-- A partial index on the coordinates, restricted to the only status the query
-- ever asks for, serves the whole predicate and stays small — searching orders
-- are a tiny, bounded slice of the table.
CREATE INDEX IF NOT EXISTS idx_orders_searching_coords
    ON orders (pickup_lat, pickup_lon)
    WHERE status = 'SEARCHING' AND pickup_lat IS NOT NULL AND pickup_lon IS NOT NULL;

-- The support chat list resolves "last message" and "last message time" per
-- chat with correlated subqueries that order by created_at and take one row.
-- Without this index each one sorts the chat's whole history.
CREATE INDEX IF NOT EXISTS idx_support_messages_chat_created
    ON support_messages (chat_id, created_at DESC);

-- The admin unread badge counts unread messages across every support chat, and
-- the admin UI asks for it on a timer. Unread messages are a small fraction of
-- the table, so a partial index turns a full scan into a scan of just those.
CREATE INDEX IF NOT EXISTS idx_support_messages_unread
    ON support_messages (chat_id, sender_id) WHERE read_at IS NULL;

-- A user's ledger history is read newest-first. idx_transactions_counterparty
-- serves the reconciliation queries, not this one.
CREATE INDEX IF NOT EXISTS idx_transactions_user_created
    ON transactions (user_id, created_at DESC);
