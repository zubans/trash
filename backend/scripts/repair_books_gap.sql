-- One-off repair: close the books gap left by movements that touched only the
-- user side.
--
-- Background. Until the ledger landed (migration 029) money moved on one side
-- only, and 029 absorbed all of that history by seeding DEPOSITS as the
-- balancing figure — so the books closed exactly at that moment. Anything open
-- now was opened afterwards, by one of the raw-SQL paths that still wrote a
-- transaction row and changed a balance without touching a system account.
--
-- Which entries are one-sided is read from transactions.counterparty, and that
-- column is only evidence from migration 060 on: before it CreateTransaction
-- dropped the value, so the ledger's own entries carry NULL just like the
-- raw-SQL ones. Entries between 029 and 060 cannot be classified and are not
-- looked at; a gap opened there makes this script refuse, and
-- locate_books_gap.sql is the next step.
--
-- What this repairs is therefore the ACCOUNT side only. The user side is
-- already correct: reconciliation reports zero balance mismatches, meaning
-- every balance agrees with that user's own transaction log. Writing another
-- transactions row would break that agreement — it would credit the user a
-- second time on paper. Only the running total on system_accounts is missing.
--
-- Nothing here decides an amount in advance. The correction is derived from the
-- rows themselves, and the script aborts rather than commit if the arithmetic
-- does not land exactly on the invariant.
--
-- Run it inside a transaction and read the output before committing:
--
--     BEGIN;
--     \i scripts/repair_books_gap.sql
--     -- read the NOTICEs, then:
--     COMMIT;   -- or ROLLBACK; to walk away

\set ON_ERROR_STOP on

-- The boundary is not inferred from the data: the first non-NULL counterparty
-- is whatever this script last repaired, not where the column became
-- trustworthy. It is the moment migration 060 ran, recorded by the migration
-- runner in the same release that started writing the column.
CREATE TEMP TABLE era ON COMMIT DROP AS
SELECT (SELECT applied_at FROM schema_migrations WHERE version = '029_system_accounts.sql')        AS ledger_start,
       (SELECT applied_at FROM schema_migrations WHERE version = '060_transaction_counterparty.sql') AS counterparty_start;

CREATE TEMP TABLE onesided ON COMMIT DROP AS
SELECT t.id, t.type::text AS type, t.amount, t.created_at
FROM transactions t, era
WHERE t.counterparty IS NULL
  AND era.counterparty_start IS NOT NULL
  AND t.created_at >= era.counterparty_start;

-- How each type should have faced an account. A user credit is an account
-- debit and the other way round, which is the whole invariant in one line.
CREATE TEMP TABLE correction ON COMMIT DROP AS
SELECT CASE o.type
           WHEN 'TOP_UP'          THEN 'DEPOSITS'
           WHEN 'REFUND'          THEN 'ESCROW'
           WHEN 'REWARD'          THEN 'ESCROW'
           WHEN 'HOLD'            THEN 'ESCROW'
           WHEN 'FINE'            THEN 'FINES'
           WHEN 'WITHDRAWAL_HOLD' THEN 'PAYOUTS'
       END AS account,
       -- Sign convention from repository/transaction.go: the account moves
       -- opposite to the balance.
       SUM(CASE o.type
               WHEN 'TOP_UP'          THEN -o.amount
               WHEN 'REFUND'          THEN -o.amount
               WHEN 'REWARD'          THEN -o.amount
               WHEN 'HOLD'            THEN  o.amount
               WHEN 'FINE'            THEN  o.amount
               WHEN 'WITHDRAWAL_HOLD' THEN  o.amount
           END) AS delta,
       count(*) AS entries
FROM onesided o
GROUP BY 1;

DO $$
DECLARE
    gap_before    NUMERIC;
    gap_after     NUMERIC;
    unmapped      INT;
    rows_found    INT;
    unclassified  INT;
    correction_ok BOOLEAN;
BEGIN
    -- Before anything else: is there a boundary at all? Without migration 060
    -- the search below returns nothing — which reads as "no one-sided entries"
    -- when the truth is that none can be recognised. Those two findings must
    -- never produce the same message.
    IF (SELECT counterparty_start FROM era) IS NULL THEN
        RAISE EXCEPTION 'migration 060 has not run, so no entry can be recognised as one-sided; deploy the release that writes transactions.counterparty first';
    END IF;

    SELECT count(*) INTO unclassified
    FROM transactions t, era
    WHERE t.created_at >= COALESCE(era.ledger_start, era.counterparty_start)
      AND t.created_at < era.counterparty_start;

    SELECT COALESCE((SELECT SUM(balance) FROM users), 0)
         + COALESCE((SELECT SUM(balance) FROM system_accounts), 0)
      INTO gap_before;

    SELECT count(*) INTO rows_found FROM onesided;
    SELECT count(*) INTO unmapped FROM correction WHERE account IS NULL;

    RAISE NOTICE 'books gap before: %', gap_before;
    RAISE NOTICE 'one-sided entries since counterparty is written: %', rows_found;
    RAISE NOTICE 'entries between migrations 029 and 060, not classifiable and not looked at: %', unclassified;

    IF gap_before = 0 THEN
        RAISE EXCEPTION 'the books already close; there is nothing to repair';
    END IF;

    -- A transaction type with no account mapping is one nobody has reasoned
    -- about. Guessing at it would move real money to the wrong place.
    IF unmapped > 0 THEN
        RAISE EXCEPTION 'there are one-sided entries of a type this script does not map to an account; inspect them before repairing';
    END IF;

    IF rows_found = 0 THEN
        RAISE EXCEPTION 'the books are open by % but no one-sided entry explains it — the cause is somewhere this script does not look (possibly between migrations 029 and 060, see locate_books_gap.sql), and a blind correction would only hide it', gap_before;
    END IF;

    -- The corrections must account for the gap exactly. Anything else means the
    -- story is incomplete, and a partial repair would leave a smaller gap that
    -- looks like a different bug.
    SELECT COALESCE(SUM(delta), 0) = -gap_before INTO correction_ok FROM correction;
    IF NOT correction_ok THEN
        RAISE EXCEPTION 'the one-sided entries account for % but the books are open by % — refusing to guess at the difference',
            (SELECT COALESCE(SUM(-delta), 0) FROM correction), gap_before;
    END IF;

    UPDATE system_accounts sa
    SET balance = sa.balance + c.delta, updated_at = now()
    FROM correction c
    WHERE sa.code = c.account;

    -- Give the offending rows their counterparty, so the history stops looking
    -- one-sided and a later audit can see they were accounted for. This no
    -- longer moves any boundary: the boundary lives in schema_migrations.
    UPDATE transactions t
    SET counterparty = CASE t.type::text
                           WHEN 'TOP_UP'          THEN 'DEPOSITS'
                           WHEN 'REFUND'          THEN 'ESCROW'
                           WHEN 'REWARD'          THEN 'ESCROW'
                           WHEN 'HOLD'            THEN 'ESCROW'
                           WHEN 'FINE'            THEN 'FINES'
                           WHEN 'WITHDRAWAL_HOLD' THEN 'PAYOUTS'
                       END
    FROM onesided o
    WHERE t.id = o.id;

    SELECT COALESCE((SELECT SUM(balance) FROM users), 0)
         + COALESCE((SELECT SUM(balance) FROM system_accounts), 0)
      INTO gap_after;

    IF gap_after <> 0 THEN
        RAISE EXCEPTION 'after the correction the books are still open by % — rolling back', gap_after;
    END IF;

    RAISE NOTICE 'books gap after: % — the books close', gap_after;
END $$;

SELECT c.account, c.entries, c.delta AS applied_to_account FROM correction c ORDER BY c.account;
SELECT code, name, balance FROM system_accounts ORDER BY code;
