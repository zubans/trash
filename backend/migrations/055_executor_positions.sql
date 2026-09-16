-- 055_executor_positions.sql
-- The executor's track: where the phone said it was, and when the server heard it.
--
-- Until now only the latest fix survived (executor_profiles.device_lat/lon,
-- migration 045): every report overwrote the previous one, and the historical
-- GPS log went away with geozones in 037. That is enough for the map and for
-- matching, both of which ask "where are they now", and not enough for
-- arbitration, which asks "where were they when this photo was taken".
--
-- Coordinates that arrive attached to a file are named by the phone itself. A
-- track is different: it is sent continuously and stamped with the server's
-- clock, so faking a single point under a photo is not enough — the whole way
-- there has to be faked too. It is still supporting evidence, not proof: the
-- executor may refuse the location permission, and then there is no track.
--
-- Rows are appended, never updated, and swept by age (executor_track_days).

CREATE TABLE IF NOT EXISTS executor_positions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    executor_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lat         DOUBLE PRECISION NOT NULL,
    lon         DOUBLE PRECISION NOT NULL,
    -- Точность фикса, если её сообщил телефон: точка с точностью в километр
    -- ничего не доказывает и не должна выглядеть доказательством.
    accuracy_m  DOUBLE PRECISION NULL,
    -- LIVE — обычный отчёт приложения, PHOTO — точка, приехавшая со снимком.
    source      VARCHAR(16) NOT NULL CHECK (source IN ('LIVE', 'PHOTO')),
    order_id    UUID NULL REFERENCES orders(id) ON DELETE SET NULL,
    -- Время устройства: по нему ищется точка, ближайшая к моменту съёмки.
    device_at   TIMESTAMPTZ NOT NULL,
    -- Время сервера: по нему видно, приехала точка сразу или пролежала в
    -- офлайн-очереди.
    reported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Ключ идемпотентности с устройства: офлайн-очередь повторяет отправку
    -- пачки, которую не смогла подтвердить.
    client_key  VARCHAR(64) NULL
);

-- Единственный вопрос сверки: точки этого исполнителя вокруг этого момента.
CREATE INDEX IF NOT EXISTS idx_executor_positions_lookup
    ON executor_positions (executor_id, device_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_executor_positions_client_key
    ON executor_positions (executor_id, client_key)
    WHERE client_key IS NOT NULL;

-- Очистка по возрасту: спор живёт днями, а таблица растёт с каждой сменой.
CREATE INDEX IF NOT EXISTS idx_executor_positions_sweep
    ON executor_positions (reported_at);

INSERT INTO system_settings (key, value) VALUES
    -- Насколько далеко по времени от съёмки может быть ближайшая точка трека,
    -- чтобы сверка считала её относящейся к снимку.
    ('photo_proof_max_track_gap_min', '15'),
    -- Сколько дней хранится трек.
    ('executor_track_days', '30')
ON CONFLICT (key) DO NOTHING;
