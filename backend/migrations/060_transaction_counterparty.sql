-- 060_transaction_counterparty.sql
-- The moment transactions.counterparty starts being written.
--
-- Ledger.record always filled Transaction.Counterparty, but CreateTransaction
-- never listed the column in its INSERT, so from migration 029 until this
-- release every row got NULL — the ledger's own two-sided entries included.
-- Ships together with the fix, and its row in schema_migrations is the
-- boundary: from applied_at on, a NULL counterparty means the entry went past
-- the Ledger. Before it, a NULL says nothing. scripts/repair_books_gap.sql and
-- scripts/locate_books_gap.sql read the boundary from there instead of guessing
-- it from the first non-NULL counterparty, which the repair itself writes.
--
-- No backfill on purpose. Rows before 029 faced no account: the seeding in 029
-- absorbed them as one lump into DEPOSITS, so NULL is the truth for them. Rows
-- between 029 and this migration cannot be told apart — the ledger's entries and
-- the one-sided raw-SQL ones fixed in August look the same — and stamping an
-- account on them by type would mark exactly the entries the repair looks for
-- as accounted for.

COMMENT ON COLUMN transactions.counterparty IS
    'System account on the other side of the entry. Written since migration 060; NULL before it says nothing, NULL after it means the entry bypassed the Ledger.';
