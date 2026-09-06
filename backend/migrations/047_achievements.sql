-- 047_achievements.sql
-- Геймификация исполнителей: ачивки-скрипты, баллы, уровни, подарки, внутренняя
-- почта — и денежная граница, без которой всё перечисленное трогать деньги не
-- имеет права (см. doc/achievements.md).
--
-- Ачивка устроена как особая услуга (043): правило живёт в Starlark-скрипте вне
-- Go-кода, скрипт возвращает просьбы, а применяет их ядро. Отличие одно и
-- принципиальное — субъект. У поведения услуги это заказ, у ачивки это
-- пользователь, поэтому у неё своя таблица выдач, свой курсор в общем outbox и
-- свой диспетчер.
--
-- Комиссию ачивка не назначает. Она приносит баллы; баллы складываются в
-- уровень; уровень снижает комиссию на фиксированный шаг. Один канал вместо
-- множества персональных скидок — потому что складывающиеся скидки это способ
-- незаметно уйти в отрицательную комиссию, то есть заплатить исполнителю
-- больше, чем заплатил заказчик.
--
-- Транзакцию эта миграция не запрещает: новых значений перечислений не
-- добавляется (BONUS появился в 043), а всё остальное — обычный DDL.

-- =============================================================================
-- 1. Два потребителя у одного outbox
-- =============================================================================

-- domain_events.processed_at один, а читателей событий становится двое:
-- поведения услуг и ачивки. Общий курсор означал бы, что кто первый обработал,
-- тот и «закрыл» событие для второго. Курсор переезжает в отдельную строку на
-- потребителя.
CREATE TABLE IF NOT EXISTS domain_event_consumers (
    event_id     UUID NOT NULL REFERENCES domain_events(id) ON DELETE CASCADE,
    consumer     VARCHAR(32) NOT NULL,
    processed_at TIMESTAMPTZ NULL,
    attempts     INT NOT NULL DEFAULT 0,
    last_error   TEXT NULL,
    PRIMARY KEY (event_id, consumer)
);

CREATE INDEX IF NOT EXISTS idx_domain_event_consumers_pending
    ON domain_event_consumers (consumer, event_id)
    WHERE processed_at IS NULL;

-- Перенос истории: всё, что было обработано (или ещё нет) до этой миграции,
-- принадлежало единственному тогда потребителю — поведениям услуг. Ачивки
-- задним числом не выдаются, поэтому события, случившиеся до их появления,
-- сразу помечаются обработанными для нового потребителя: иначе первый же тик
-- диспетчера прогнал бы по ним всю историю платформы.
INSERT INTO domain_event_consumers (event_id, consumer, processed_at, attempts, last_error)
SELECT id, 'behaviors', processed_at, attempts, last_error FROM domain_events
ON CONFLICT DO NOTHING;

INSERT INTO domain_event_consumers (event_id, consumer, processed_at)
SELECT id, 'achievements', COALESCE(processed_at, now()) FROM domain_events
ON CONFLICT DO NOTHING;

-- =============================================================================
-- 2. Денежные инциденты
-- =============================================================================

-- Расчёт, давший сумму больше уплаченной заказчиком, не откатывается: откат
-- оставил бы заказ подтверждённым в одной половине системы и неоплаченным в
-- другой, а повтор упёрся бы в ту же ошибку. Он зажимается — и оставляет здесь
-- след, по которому причину найдут.
--
-- Строка пишется в той же транзакции, что и зажатое движение: инцидент не может
-- закоммититься без движения, а движение — без инцидента.
CREATE TABLE IF NOT EXISTS money_incidents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind        VARCHAR(48) NOT NULL,
    severity    VARCHAR(16) NOT NULL DEFAULT 'CRITICAL',
    order_id    UUID NULL REFERENCES orders(id) ON DELETE SET NULL,
    user_id     UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    -- Суммы в копейках, как и везде в деньгах: что должно было быть, что
    -- посчитал код и что в итоге записано после зажима.
    expected    BIGINT NULL,
    actual      BIGINT NULL,
    applied     BIGINT NULL,
    details     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ NULL,
    resolved_by UUID NULL REFERENCES users(id),
    resolution  TEXT NULL
);

CREATE INDEX IF NOT EXISTS idx_money_incidents_open
    ON money_incidents (created_at DESC)
    WHERE resolved_at IS NULL;

-- Ставка, по которой заказ реально закрыли, и её источник. Без них через месяц
-- никто не объяснит, почему по двум одинаковым заказам разная комиссия: ставка
-- теперь зависит от уровня исполнителя на момент подтверждения.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS commission_percent NUMERIC(5,2) NULL;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS commission_level INT NULL;

-- =============================================================================
-- 3. Ачивки
-- =============================================================================

-- Каталог. Ачивки бывают двух происхождений, и различие между ними — в том,
-- где лежит их скрипт:
--
--   поставляемая — код совпадает с именем каталога в бинарнике
--                  (backend/achievements/<код>), source пуст, править скрипт из
--                  админки нельзя: правило, меняющее деньги, проходит ревью;
--   собственная  — создана в админ-панели, скрипт лежит здесь, в source и
--                  constants, и компилируется при старте и по таймеру.
--
-- Строка в обоих случаях — то, чем админ управляет, не пересобирая образ:
-- включена ли, в каком окне выдаётся, сколько весит.
CREATE TABLE IF NOT EXISTS achievements (
    code           VARCHAR(64) PRIMARY KEY,
    is_active      BOOLEAN NOT NULL DEFAULT FALSE,
    -- Окно акции: когда ачивку вообще можно заслужить. NULL с обеих сторон —
    -- выдаётся всегда.
    available_from TIMESTAMPTZ NULL,
    available_to   TIMESTAMPTZ NULL,
    -- Переопределение веса из скрипта. NULL — берётся MANIFEST.weight, а если и
    -- его нет — настройка achievement_default_weight.
    weight         INT NULL,
    config         JSONB NOT NULL DEFAULT '{}'::jsonb,
    sort_order     INT NOT NULL DEFAULT 0,
    -- Собственный скрипт: константы и логика, ровно те же два файла, что у
    -- поставляемой ачивки, только хранятся в базе.
    constants      TEXT NOT NULL DEFAULT '',
    source         TEXT NOT NULL DEFAULT '',
    -- Удаление мягкое, и это не осторожность ради осторожности: у ачивки есть
    -- выданные экземпляры и начисленные по ним баллы, то есть чей-то уровень и
    -- чья-то ставка комиссии. Жёсткое удаление либо стирало бы историю, либо
    -- оставляло баллы, ссылающиеся в пустоту.
    deleted_at     TIMESTAMPTZ NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_achievements_live
    ON achievements (sort_order, code) WHERE deleted_at IS NULL;

-- Выданное. grant_key у разовой ачивки равен её коду, у повторяемой — тому, что
-- вернул скрипт: id заказа, месяц, номер серии. Уникальность здесь и есть защита
-- от повторной выдачи, а не проверка в сервисе: переотправленное событие
-- проигрывает вставку, а не начисляет баллы второй раз.
--
-- points — снимок веса на момент выдачи, а не ссылка на achievements.weight.
-- Иначе правка веса в админке молча пересчитала бы уровни, а с ними и комиссию,
-- всем, кто эту ачивку когда-либо получал.
CREATE TABLE IF NOT EXISTS user_achievements (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- RESTRICT, а не CASCADE: удаление ачивки не должно уносить с собой то, что
    -- люди по ней получили. Удаление и так мягкое (achievements.deleted_at), и
    -- эта связь — вторая половина того же решения.
    code          VARCHAR(64) NOT NULL REFERENCES achievements(code) ON DELETE RESTRICT,
    grant_key     VARCHAR(120) NOT NULL,
    points        INT NOT NULL DEFAULT 0,
    order_id      UUID NULL REFERENCES orders(id) ON DELETE SET NULL,
    granted_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- NULL — вечная. Значок остаётся в профиле и после срока; истекают баллы.
    expires_at    TIMESTAMPTZ NULL,
    revoked_at    TIMESTAMPTZ NULL,
    revoke_reason TEXT NULL,
    UNIQUE (user_id, code, grant_key)
);

CREATE INDEX IF NOT EXISTS idx_user_achievements_active
    ON user_achievements (user_id) WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_achievements_order
    ON user_achievements (order_id) WHERE order_id IS NOT NULL;

-- Баллы отдельным реестром, потому что ачивки — не единственный их источник:
-- начисления за объём, ручная корректировка администратора, будущие акции
-- пишут сюда же. Уровень считается по этой таблице, а user_achievements — лишь
-- один из источников её строк.
--
-- expires_at и revoked_at повторяют поля выдачи намеренно: сумма действующих
-- баллов должна вычисляться одним запросом по одной таблице, без join'а с тем,
-- откуда балл пришёл.
CREATE TABLE IF NOT EXISTS user_points (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    points      INT NOT NULL,
    source_type VARCHAR(32) NOT NULL,   -- achievement | admin | promo
    source_code VARCHAR(64) NOT NULL DEFAULT '',
    source_id   UUID NULL,              -- user_achievements.id, когда источник — ачивка
    reason      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NULL,
    revoked_at  TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_user_points_active
    ON user_points (user_id) WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_points_source
    ON user_points (source_id) WHERE source_id IS NOT NULL;

-- Дневной потолок начисления. Проверка и начисление коммитятся вместе, поэтому
-- два одновременных события не пробьют потолок вдвоём.
CREATE TABLE IF NOT EXISTS user_points_daily (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day     DATE NOT NULL,
    points  INT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day)
);

-- Агрегаты исполнителя. Скрипт ачивки — чистая функция и сам в базу не ходит:
-- всё, что ему нужно знать о прошлом, ядро приносит отсюда одним чтением по
-- первичному ключу. Считать эти же числа запросами на каждое событие — самая
-- дорогая часть механизма, поэтому они обновляются инкрементально там, где
-- меняются.
CREATE TABLE IF NOT EXISTS executor_stats (
    user_id                UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    orders_completed       INT NOT NULL DEFAULT 0,
    orders_completed_month INT NOT NULL DEFAULT 0,
    -- Месяц, к которому относится счётчик выше: сравнение с текущим и есть
    -- обнуление, поэтому отдельный ночной проход не нужен.
    month_key              CHAR(7) NOT NULL DEFAULT '',
    distinct_customers     INT NOT NULL DEFAULT 0,
    fastest_completion_min INT NULL,
    five_star_streak       INT NOT NULL DEFAULT 0,
    rating_count           INT NOT NULL DEFAULT 0,
    cancels                INT NOT NULL DEFAULT 0,
    earned_total           BIGINT NOT NULL DEFAULT 0,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Заказчики, с которыми исполнитель уже работал. Нужны ровно для одного:
-- ачивка с подарком вправе требовать N разных заказчиков, иначе она
-- зарабатывается сговором с одним и тем же (doc/achievements.md §9).
CREATE TABLE IF NOT EXISTS executor_customers (
    executor_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    customer_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    orders      INT NOT NULL DEFAULT 0,
    last_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (executor_id, customer_id)
);

-- =============================================================================
-- 4. Подарки
-- =============================================================================

CREATE TABLE IF NOT EXISTS gifts (
    code        VARCHAR(64) PRIMARY KEY,
    -- BONUS      — деньги на счёт, со счёта платформы BONUSES;
    -- CERTIFICATE— код интернет-магазина из пула;
    -- PHYSICAL   — вещь: купон в профиле, который гасит администратор при выдаче;
    -- PROMO      — общий промокод партнёра, один на всех.
    kind        VARCHAR(16) NOT NULL CHECK (kind IN ('BONUS', 'CERTIFICATE', 'PHYSICAL', 'PROMO')),
    title       JSONB NOT NULL DEFAULT '{}'::jsonb,
    description JSONB NOT NULL DEFAULT '{}'::jsonb,
    image_url   TEXT NULL,
    -- BONUS: сумма начисления в копейках. Остальные: номинал, справочно.
    amount      BIGINT NOT NULL DEFAULT 0,
    partner     VARCHAR(64) NULL,
    -- Общий промокод для PROMO. Для CERTIFICATE коды лежат в пуле ниже.
    promo_code  TEXT NULL,
    -- Остаток. NULL — не ограничен. Для CERTIFICATE считается по свободным кодам.
    stock       INT NULL,
    -- Сколько живёт купон с момента выдачи. NULL — бессрочно.
    valid_days  INT NULL,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Пул кодов сертификатов. Код достаётся ровно одному: захват — это
-- UPDATE ... WHERE issued_to IS NULL ... RETURNING в транзакции выдачи, поэтому
-- два одновременных события не заберут один код.
CREATE TABLE IF NOT EXISTS gift_codes (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    gift_code VARCHAR(64) NOT NULL REFERENCES gifts(code) ON DELETE CASCADE,
    secret    TEXT NOT NULL,
    issued_to UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    issued_at TIMESTAMPTZ NULL,
    UNIQUE (gift_code, secret)
);

CREATE INDEX IF NOT EXISTS idx_gift_codes_free
    ON gift_codes (gift_code) WHERE issued_to IS NULL;

-- Что человек получил. coupon_code уникален по всей таблице: его вводит
-- администратор, когда выдаёт вещь, и он не должен совпасть у двух подарков.
CREATE TABLE IF NOT EXISTS user_gifts (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    gift_code      VARCHAR(64) NOT NULL REFERENCES gifts(code),
    gift_code_id   UUID NULL REFERENCES gift_codes(id) ON DELETE SET NULL,
    achievement_id UUID NULL REFERENCES user_achievements(id) ON DELETE SET NULL,
    coupon_code    VARCHAR(32) NOT NULL UNIQUE,
    status         VARCHAR(16) NOT NULL DEFAULT 'ISSUED'
                   CHECK (status IN ('ISSUED', 'REVEALED', 'REDEEMED', 'EXPIRED', 'CANCELED')),
    fulfillment    JSONB NOT NULL DEFAULT '{}'::jsonb,
    granted_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at     TIMESTAMPTZ NULL,
    revealed_at    TIMESTAMPTZ NULL,
    redeemed_at    TIMESTAMPTZ NULL,
    redeemed_by    UUID NULL REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_user_gifts_user ON user_gifts (user_id, granted_at DESC);

-- =============================================================================
-- 5. Внутренняя почта
-- =============================================================================

-- Ящик пользователя: сюда приходит всё, что платформа говорит человеку вне
-- заказа, — полученная ачивка, купон на подарок, акция, новость.
--
-- Почему не чат: чат привязан к заказу и двусторонен, а это односторонняя
-- лента, которая должна пережить закрытие заказа и существовать у пользователя,
-- у которого заказов нет вовсе. Почему не e-mail: письмо уходит наружу и может
-- не дойти, а купон обязан быть там же, где приложение.
CREATE TABLE IF NOT EXISTS user_mail (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- ACHIEVEMENT | GIFT | PROMO | NEWS | SYSTEM — от типа зависит иконка и то,
    -- какую карточку рисует приложение.
    kind       VARCHAR(16) NOT NULL DEFAULT 'SYSTEM'
               CHECK (kind IN ('ACHIEVEMENT', 'GIFT', 'PROMO', 'NEWS', 'SYSTEM')),
    subject    TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    -- Ссылка на то, о чём письмо: код ачивки, id подарка. Приложение по нему
    -- открывает нужный экран, а не разбирает текст.
    ref_type   VARCHAR(24) NOT NULL DEFAULT '',
    ref_id     TEXT NOT NULL DEFAULT '',
    -- Отправитель рассылки, если письмо создал администратор.
    sender_id  UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at    TIMESTAMPTZ NULL,
    deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_user_mail_inbox
    ON user_mail (user_id, created_at DESC) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_mail_unread
    ON user_mail (user_id) WHERE read_at IS NULL AND deleted_at IS NULL;

-- =============================================================================
-- 6. Настройки
-- =============================================================================

INSERT INTO system_settings (key, value) VALUES
    -- Баллов на один уровень. Уровень снижает комиссию на шаг ниже.
    ('achievement_level_points', '500'),
    -- Процентных пунктов комиссии за уровень. Итог всегда зажат в [0, базовая].
    ('achievement_level_discount_pp', '1'),
    -- Вес ачивки, не назначившей свой.
    ('achievement_default_weight', '5'),
    -- Потолок баллов в сутки на пользователя: столько может стоить накрутка,
    -- если её не заметят до конца дня.
    ('achievement_max_points_per_day', '50'),
    -- Потолок одного денежного подарка, в рублях.
    ('achievement_max_bonus', '5000'),
    -- Минимальная сумма заказа, с которого ачивка вообще засчитывается.
    ('achievement_min_order_amount', '300')
ON CONFLICT (key) DO NOTHING;

-- =============================================================================
-- 7. Поставляемые ачивки
-- =============================================================================

-- Выключены: раздача баллов, снижающих комиссию, — решение администратора, а не
-- миграции. Скрипты уже в бинарнике, строки лишь разрешают им срабатывать.
INSERT INTO achievements (code, is_active, sort_order) VALUES
    ('first_order', FALSE, 10),
    ('fastest_gun', FALSE, 20),
    ('marathon_month', FALSE, 30)
ON CONFLICT (code) DO NOTHING;
