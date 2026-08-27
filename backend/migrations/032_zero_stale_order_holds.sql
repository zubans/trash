-- Zero the hold left standing on orders that finished before the hold was
-- being zeroed at all.
--
-- Until d3645b05 (2026-08-24 14:38 +03) confirming or cancelling an order paid
-- the executor or refunded the customer but left orders.hold_amount as it was.
-- The money moved correctly — every user balance still agrees with their own
-- transaction log — but the column kept claiming a hold that no longer existed.
-- Thirty-four such rows survived, together claiming 5720.00 while the escrow
-- account held 300.00, which is the whole proof that nothing is actually stuck:
-- if those holds were real, escrow would have to contain them.
--
-- Reconciliation reports each one as "finished order still holds money", so the
-- nightly check has been red on historical noise. That is the reason to clean
-- them: thirty-four rows of known noise will hide the thirty-fifth, which would
-- be a real one.
--
-- The cut-off is deliberate rather than "every finished order". A migration
-- that zeroed the column unconditionally would also erase evidence of any
-- future regression that reintroduces the bug. Bounded to the period before the
-- fix, it cleans history and leaves the detector intact.

CREATE TABLE IF NOT EXISTS order_hold_cleanup_archive (
    order_id     UUID PRIMARY KEY,
    status       TEXT        NOT NULL,
    hold_amount  NUMERIC(18,2) NOT NULL,
    finished_at  TIMESTAMPTZ,
    archived_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE order_hold_cleanup_archive IS
    'Values of orders.hold_amount cleared by migration 032. Kept so the historical figures stay reconstructable; no money moved.';

INSERT INTO order_hold_cleanup_archive (order_id, status, hold_amount, finished_at)
SELECT o.id,
       o.status::text,
       o.hold_amount,
       COALESCE(o.completed_at, o.canceled_at)
FROM orders o
WHERE o.status IN ('COMPLETED', 'CANCELED')
  AND o.hold_amount > 0
  AND COALESCE(o.completed_at, o.canceled_at, o.created_at) < TIMESTAMPTZ '2026-08-24 14:38+03'
ON CONFLICT (order_id) DO NOTHING;

UPDATE orders o
SET hold_amount = 0
WHERE o.status IN ('COMPLETED', 'CANCELED')
  AND o.hold_amount > 0
  AND COALESCE(o.completed_at, o.canceled_at, o.created_at) < TIMESTAMPTZ '2026-08-24 14:38+03';
