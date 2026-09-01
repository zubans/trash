-- The executor's working anchor and the position their phone reports are two
-- different things, and until now they shared one pair of columns. A periodic
-- GPS report therefore overwrote a district the executor had chosen by hand,
-- dragging their work area back on its own.
--
-- current_lat/current_lon keep their meaning: the anchor the map centres on and
-- that order visibility and matching measure against. The device fix gets its
-- own columns, so it can go on being recorded without moving that anchor.
ALTER TABLE executor_profiles
    ADD COLUMN IF NOT EXISTS device_lat NUMERIC(18, 8),
    ADD COLUMN IF NOT EXISTS device_lon NUMERIC(18, 8),
    ADD COLUMN IF NOT EXISTS device_reported_at TIMESTAMP WITH TIME ZONE;

-- Executors who have never moved their marker by hand keep following their
-- device, so seed the device fix from the position already stored for them.
UPDATE executor_profiles
   SET device_lat = current_lat,
       device_lon = current_lon
 WHERE device_lat IS NULL
   AND current_lat IS NOT NULL
   AND current_lon IS NOT NULL;
