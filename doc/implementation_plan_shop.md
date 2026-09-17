# План реализации: магазин

Статус: **проект**. Разбивка на задачи —
[`tasks_shop.md`](./tasks_shop.md).

Магазин — витрина внутри приложения, где заказчик и исполнитель покупают
товары за деньги со своего внутреннего баланса. Товары двух природ:

- **привилегии** — цифровой товар, который меняет правила платформы для
  покупателя. Первый такой товар — «Комиссия вдвое меньше» за 1000 ₽;
- **вещи и коды** — мерч (футболка, термокружка) и сертификаты партнёров.

Каталог, склад, цены и обработка заказов — из админ-панели.

---

## 1. Что уже есть и что берём

Магазин почти целиком собирается из готовых частей. Новое в нём — цена,
витрина и привилегии; выдача вещей и кодов уже решена подарками ачивок.

| Нужно магазину | Что уже есть | Где |
| :--- | :--- | :--- |
| Списать деньги с баланса, не уводя его в минус | `Ledger.Reserve` — дебет с проверкой баланса | `backend/service/ledger.go` |
| Выручка на отдельном счёте и её вывод | счёт `COMMISSION` и `Ledger.Payout` — тот же приём | [`financial_system.md`](./financial_system.md#4b-комиссия-платформы) |
| Склад вещей, пул кодов, купон, погашение при выдаче | `gifts`, `gift_codes`, `user_gifts`, `giftRepo.Issue` / `RedeemCoupon` | [`achievements.md`](./achievements.md#7-подарки), `backend/repository/gift.go` |
| Экран «мои купоны» | `GiftsPage.vue` | `frontend/src/pages/executor/` |
| Ставка комиссии исполнителя в одном месте | `Levels.For` → `Level.Percent`, ставка пишется в заказ | `backend/service/achievement.go` |
| Уведомления в приложении | внутренняя почта `user_mail` | [`achievements.md`](./achievements.md) |
| Права на разделы админки | каталог разделов `permissionCatalog` | [`roles.md`](./roles.md) |
| События для ачивок | outbox `domain_events` | `backend/repository/domain_event.go` |
| Проверка сходимости книг | сверка по системным счетам | `GET /admin/finances/reconciliation` |

Отсюда главное решение: **товар магазина — не новая сущность выдачи, а цена
над тем, что платформа уже умеет выдавать.** Вещь и код выдаются как подарок
(`giftRepo.Issue`), только оплаченный. Новый механизм нужен одному роду товара —
привилегии.

---

## 2. Товары

### 2.1 Роды товара

| Род | Что покупатель получает | Чем выдаётся | Остаток |
| :--- | :--- | :--- | :--- |
| `PERK` | привилегия на срок: комиссия ×0.5 на 30 дней | строка `user_perks` | не ограничен |
| `PHYSICAL` | вещь; купон, который гасит администратор при выдаче или отправке | `giftRepo.Issue` по подарку `PHYSICAL` | склад подарка `gifts.stock` |
| `CERTIFICATE` | код партнёра, показывается по запросу владельца | `giftRepo.Issue` по подарку `CERTIFICATE` | свободные коды `gift_codes` |

`BONUS` и `PROMO` в магазин не попадают: купить деньги за деньги бессмысленно,
а общий промокод на всех не товар.

Товар рода `PHYSICAL` / `CERTIFICATE` ссылается на подарок (`gift_code`).
Склад один на ачивки и магазин: футболка, выданная за «Марафонца», и футболка,
проданная за 1500 ₽, лежат на одной полке, и считать их порознь — значит
рано или поздно продать то, чего нет.

### 2.2 Карточка товара

- название и описание `{ru, en}` — как у узлов каталога и подарков;
- изображения (до 5), порядок;
- цена в копейках (`money.Amount`), необязательная «старая цена» для зачёркивания;
- **кому виден и доступен** — набор ролей (`EXECUTOR`, `CUSTOMER`, …).
  Привилегия на комиссию имеет смысл только исполнителю;
- категория (Привилегии / Мерч / Сертификаты) и порядок сортировки;
- лимит на покупателя: всего и одновременно активных (для `PERK`);
- требуется ли верификация аккаунта;
- для `PHYSICAL` — варианты (размер) и способы получения;
- `is_active` — снять с витрины, не удаляя. Товар с продажами не удаляется:
  на него ссылаются покупки.

---

## 3. Привилегия «комиссия вдвое меньше»

### 3.1 Параметры, которые товар задаёт

| Поле | Пример | Смысл |
| :--- | :--- | :--- |
| `perk_kind` | `COMMISSION_MULTIPLIER` | что меняет привилегия; пока единственный вид |
| `perk_value` | `0.5` | множитель ставки, `0 < value ≤ 1` |
| `perk_days` | `30` | срок действия с момента начала |

**Бессрочной привилегии нет.** Разовые 1000 ₽ за половину комиссии навсегда —
это продажа доли выручки платформы по фиксированной цене: исполнитель с
оборотом 100 000 ₽ в месяц при ставке 10 % окупает её за три дня и дальше
платит вдвое меньше годами. Срок — обязательное поле, валидатор не пропускает
пустое.

### 3.2 Формула

```
level_percent = clamp(base − level × discount_pp, 0, base)    # как сейчас
percent       = level_percent × multiplier                      # новое
```

- Множитель применяется **после** уровня. Так «вдвое меньше» остаётся вдвое
  меньше той комиссии, которую исполнитель платил бы сейчас, — ровно то, что
  написано на витрине.
- Множитель, а не вычитание процентных пунктов: если админ поднимет базовую
  ставку с 10 % до 15 %, купленная привилегия даёт 7.5 %, а не остаётся на
  старых 5 %. Обещание «вдвое» выполняется при любой ставке.
- Считается там же, где уровень, — в `Levels.For`. Это единственная точка, где
  определяется ставка исполнителя, и через неё уже ходят
  `OrderService.commissionLevel` (подтверждение заказа) и решение спора
  «неизвестно» (`dispute.go`). Отдельный вызов в каждом месте разошёлся бы при
  первом новом пути денег.
- Зажим `[0, base]` остаётся в Go; множитель вне `(0, 1]` отбрасывается с
  инцидентом, а не применяется.

`Level` получает поля `PerkMultiplier` и `PerkID`; в заказ вместе с
`commission_percent` и `commission_level` пишется `commission_perk_id`. Через
месяц по двум одинаковым заказам с разной комиссией ответ на «почему» должен
читаться из самого заказа.

### 3.3 Что делать с повторной покупкой

Две привилегии одновременно **не складываются** в ×0.25: это превращало бы
витрину в способ обнулить комиссию за 2000 ₽. Повторная покупка **продлевает**:
новая привилегия начинается там, где кончается последняя из купленных.

```
starts_at  = max(now(), последний expires_at активных привилегий того же вида)
expires_at = starts_at + perk_days
```

Действует привилегия, у которой `starts_at ≤ now() < expires_at` и
`revoked_at IS NULL`. Если по ошибке админа их окажется несколько, берётся
наименьший множитель одной — не произведение.

Лимит «одновременно купленных вперёд» (`max_active_per_user`, по умолчанию 3)
не даёт выкупить год заранее по старой цене перед подорожанием.

### 3.4 Где множитель не действует

- **Вознаграждения из `BONUSES`** (`commissionOnBonus`) берут базовую ставку без
  уровня — и без привилегии тоже. Это решение уже принято для уровней, и
  привилегия ему следует.
- **Чаевые** комиссией не облагаются вовсе.
- **Заказы, подтверждённые до покупки**, не пересчитываются: ставка
  фиксируется в момент подтверждения.

Привилегия считается по моменту **подтверждения** заказа, а не взятия: заказ,
взятый вчера и подтверждённый сегодня после покупки, закрывается по сниженной
ставке. На витрине это пишется явно.

### 3.5 Честная витрина

- Карточка привилегии показывает текущую ставку покупателя и ставку с
  привилегией: «Сейчас 7 % → с привилегией 3.5 %».
- Ниже — окупаемость по его собственной комиссии за последние 30 дней из
  проводок `COMMISSION`: «за прошлый месяц вы заплатили 2 400 ₽ комиссии;
  привилегия сэкономила бы 1 200 ₽».
- **Если текущая ставка покупателя уже 0 %** (базовая ставка 0 или уровень
  снял всё), покупка отклоняется с `409` и понятным текстом. Продавать
  половину от нуля — прямой обман; предупреждения тут недостаточно.
- На дашборде исполнителя и странице ачивок — плашка активной привилегии с
  датой окончания; за 3 дня до конца — письмо во внутреннюю почту.

---

## 4. Покупка

### 4.1 Деньги

Новый системный счёт **`SHOP`** — выручка магазина. Три типа проводок:

| Тип | Счёт | Знак | Смысл |
| :--- | :--- | :--- | :--- |
| `SHOP_PURCHASE` | пользователь → `SHOP` | `-1` | оплата покупки |
| `SHOP_REFUND` | `SHOP` → пользователь | `+1` | возврат при отмене |
| `SHOP_PAYOUT` | `SHOP` → `DEPOSITS` | `0` | администратор вывел выручку |

- Оплата — `Ledger.Reserve`: баланс не может уйти в минус.
  `min_balance_limit`, который позволяет исполнителю быть в минусе ради
  штрафов, к магазину не относится — купить в долг нельзя.
- Выводу выручки — `Ledger.Payout` с `DebitAvailable`, как у комиссии: два
  одновременных вывода не заберут больше, чем собрано.
- Возврат — `Ledger.Release` из `SHOP`. Если выручку уже вывели и на счёте
  меньше суммы возврата, `SHOP` уходит в минус: возврат покупателю — обязанность
  платформы, а не функция остатка на счёте. Сверка покажет минус, это и есть
  сигнал.
- Сверка получает `SHOP` в разбивке по счетам.
- Комиссия с продаж магазина не берётся: продаёт сама платформа.

Оплата только с внутреннего баланса. Не хватает — кнопка «Пополнить» ведёт на
обычную заявку `TOPUP`. Эквайринг — отдельная задача вне этого плана.

### 4.2 Одна транзакция

```
BEGIN
  SELECT … FROM shop_products WHERE id = $1 FOR UPDATE      -- цена, лимиты, активность
  проверки: роль, верификация, статус пользователя, лимит на покупателя,
            цена совпала с expected_price, для PERK — текущая ставка > 0
  INSERT shop_orders (… request_id …)                       -- уникальность = идемпотентность
  Ledger.Reserve(user → SHOP, SHOP_PURCHASE, price × qty)
  выдача:
    PERK        → INSERT user_perks (starts_at, expires_at по §3.3)
    PHYSICAL    → giftRepo.Issue (снимает stock) → user_gifts
    CERTIFICATE → giftRepo.Issue (захватывает код)  → user_gifts
  INSERT domain_events ('shop.purchased')
COMMIT
→ после коммита: письмо во внутреннюю почту, счётчик новых заказов админу
```

Правила, которые держит транзакция:

- **Идемпотентность.** Клиент генерирует `request_id` (UUID) при открытии окна
  оформления; `UNIQUE (user_id, request_id)`. Повтор запроса после обрыва сети
  возвращает ту же покупку, а не списывает второй раз.
- **Цена, которую видел покупатель.** Запрос несёт `expected_price`. Админ
  поменял цену между показом и нажатием — `409 price_changed`, клиент
  перерисовывает карточку. В покупку пишется снимок цены и названия.
- **Нет товара — нет денег.** В отличие от ачивок, где пустой склад — мягкий
  отказ (ачивка выдаётся без подарка), здесь `ErrGiftUnavailable` откатывает
  всю транзакцию и возвращает `409 out_of_stock`. Взять деньги и не выдать
  купленное нельзя.
- **Кто не может покупать:** `BANNED`, `SOFT_BANNED` (мягкий бан оставляет
  доступ к балансу, но не к тратам — магазин не попадает в разрешённый список
  `middleware/soft_ban.go`), пользователи без нужной роли; для товаров с
  флагом — неверифицированные.
- **Количество** — для `PHYSICAL` до `max_qty_per_order` (по умолчанию 5), для
  `PERK` и `CERTIFICATE` всегда 1.
- Выключатель `shop_enabled` в системных настройках: `0` прячет витрину и
  отклоняет покупки, админка остаётся доступной.

### 4.3 Статусы покупки

```mermaid
stateDiagram-v2
    [*] --> PAID
    PAID --> COMPLETED: PERK и CERTIFICATE — сразу
    PAID --> PROCESSING: PHYSICAL — админ взял в работу
    PROCESSING --> SHIPPED: отправлено (трек) / готово к выдаче
    SHIPPED --> COMPLETED: выдано — погашен купон
    PAID --> CANCELED: отмена
    PROCESSING --> CANCELED: отмена
```

| Отмена | Кто | Деньги | Выдача |
| :--- | :--- | :--- | :--- |
| `PHYSICAL` в `PAID` | покупатель или админ | полный возврат | купон `CANCELED`, `stock + qty` |
| `PHYSICAL` в `PROCESSING` | только админ | полный возврат | купон `CANCELED`, склад — по галочке «вернуть на склад» |
| `PERK` | только админ | сумма указывается админом, по умолчанию пропорционально неиспользованным дням | `user_perks.revoked_at` |
| `CERTIFICATE` после показа кода | никто | — | код уже у покупателя |

Каждая отмена админом требует причину и пишет `[AUDIT]` с его id, как вывод
комиссии. Возврат и снятие выдачи — в одной транзакции.

---

## 5. Получение вещей

Способы получения задаются на товаре, админ включает нужные:

| Способ | Что спрашиваем при оформлении | Что делает админ |
| :--- | :--- | :--- |
| `PICKUP` | пункт выдачи из справочника | по купону на месте гасит его → `COMPLETED` |
| `DELIVERY` | адрес (подсказки DaData, как у заказов), получатель, телефон | вносит трек-номер → `SHIPPED`, затем `COMPLETED` |

Адрес и размер ложатся в `user_gifts.fulfillment` — поле для этого там уже
есть. Стоимость доставки в первой версии включается в цену товара; отдельная
строка доставки — открытый вопрос (§9).

Справочник пунктов выдачи — таблица `shop_pickup_points` (название, адрес,
часы работы, `is_active`).

---

## 6. Схема данных

Миграций две: добавление значений enum не работает внутри транзакции
(см. `038_order_commission.sql`), а таблицы лучше создавать в ней.

**`058_shop_money.sql`** (`-- +migrate no-transaction`):

```sql
ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'SHOP_PURCHASE';
ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'SHOP_REFUND';
ALTER TYPE transaction_type ADD VALUE IF NOT EXISTS 'SHOP_PAYOUT';

INSERT INTO system_accounts (code, name) VALUES ('SHOP', 'Выручка магазина')
ON CONFLICT (code) DO NOTHING;
```

**`059_shop.sql`**:

```sql
CREATE TABLE shop_products (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind                VARCHAR(16) NOT NULL CHECK (kind IN ('PERK', 'PHYSICAL', 'CERTIFICATE')),
    category            VARCHAR(32) NOT NULL,
    title               JSONB NOT NULL,
    description         JSONB NOT NULL DEFAULT '{}'::jsonb,
    images              JSONB NOT NULL DEFAULT '[]'::jsonb,        -- ["/uploads/shop/…"]
    price               BIGINT NOT NULL CHECK (price > 0),          -- копейки
    compare_at_price    BIGINT NULL,
    roles               TEXT[] NOT NULL DEFAULT '{}',               -- пусто = все
    requires_verified   BOOLEAN NOT NULL DEFAULT FALSE,
    per_user_limit      INT NULL,                                   -- NULL = без лимита
    max_qty_per_order   INT NOT NULL DEFAULT 1,
    -- PHYSICAL / CERTIFICATE
    gift_code           VARCHAR(64) NULL REFERENCES gifts(code),
    variants            JSONB NOT NULL DEFAULT '[]'::jsonb,         -- [{"code":"M","title":{…}}]
    fulfillment_methods TEXT[] NOT NULL DEFAULT '{}',               -- PICKUP, DELIVERY
    -- PERK
    perk_kind           VARCHAR(32) NULL CHECK (perk_kind IN ('COMMISSION_MULTIPLIER')),
    perk_value          NUMERIC(5,4) NULL CHECK (perk_value > 0 AND perk_value <= 1),
    perk_days           INT NULL CHECK (perk_days > 0),
    max_active_per_user INT NULL,
    sort_order          INT NOT NULL DEFAULT 0,
    is_active           BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (kind <> 'PERK' OR (perk_kind IS NOT NULL AND perk_value IS NOT NULL AND perk_days IS NOT NULL)),
    CHECK (kind = 'PERK' OR gift_code IS NOT NULL)
);

CREATE TABLE shop_orders (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    number          BIGSERIAL UNIQUE,                   -- «Заказ №1042» для людей
    user_id         UUID NOT NULL REFERENCES users(id),
    request_id      UUID NOT NULL,
    product_id      UUID NOT NULL REFERENCES shop_products(id),
    product_snapshot JSONB NOT NULL,                    -- название, род, параметры привилегии
    variant         VARCHAR(32) NULL,
    quantity        INT NOT NULL CHECK (quantity > 0),
    unit_price      BIGINT NOT NULL,
    total           BIGINT NOT NULL,
    status          VARCHAR(16) NOT NULL
                    CHECK (status IN ('PAID', 'PROCESSING', 'SHIPPED', 'COMPLETED', 'CANCELED')),
    fulfillment     JSONB NOT NULL DEFAULT '{}'::jsonb, -- способ, пункт/адрес, трек
    refunded_amount BIGINT NOT NULL DEFAULT 0,
    cancel_reason   TEXT NULL,
    canceled_by     UUID NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, request_id)
);
CREATE INDEX idx_shop_orders_user ON shop_orders (user_id, created_at DESC);
CREATE INDEX idx_shop_orders_status ON shop_orders (status, created_at);

-- Купоны покупки: одна строка user_gifts на единицу товара.
ALTER TABLE user_gifts ADD COLUMN IF NOT EXISTS shop_order_id UUID NULL REFERENCES shop_orders(id);

CREATE TABLE user_perks (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          VARCHAR(32) NOT NULL,
    value         NUMERIC(5,4) NOT NULL,
    starts_at     TIMESTAMPTZ NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    shop_order_id UUID NULL REFERENCES shop_orders(id),  -- NULL — выдана админом
    revoked_at    TIMESTAMPTZ NULL,
    revoked_by    UUID NULL REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expires_at > starts_at)
);
CREATE INDEX idx_user_perks_active ON user_perks (user_id, kind, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE shop_pickup_points (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title     JSONB NOT NULL,
    address   TEXT NOT NULL,
    hours     TEXT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

ALTER TABLE orders ADD COLUMN IF NOT EXISTS commission_perk_id UUID NULL REFERENCES user_perks(id);

INSERT INTO system_settings (key, value) VALUES ('shop_enabled', '0')
ON CONFLICT (key) DO NOTHING;
```

Всё создаётся выключенным: `shop_enabled = 0`, товары `is_active = FALSE`.
Открыть магазин и назначить цену — решение администратора, а не миграции.

Запрос привилегии на пути подтверждения заказа — один, по частичному индексу:

```sql
SELECT id, value FROM user_perks
 WHERE user_id = $1 AND kind = 'COMMISSION_MULTIPLIER'
   AND revoked_at IS NULL AND starts_at <= now() AND expires_at > now()
 ORDER BY value ASC LIMIT 1;
```

---

## 7. API

### Покупатель (`RequireAuth`, любая роль)

| Метод | Путь | Что делает |
| :--- | :--- | :--- |
| `GET` | `/api/shop/products` | витрина: активные товары, доступные ролям пользователя; `?category=` |
| `GET` | `/api/shop/products/{id}` | карточка; для `PERK` — текущая ставка, ставка с привилегией, окупаемость (§3.5) |
| `POST` | `/api/shop/orders` | покупка: `{product_id, request_id, expected_price, quantity, variant, fulfillment}` |
| `GET` | `/api/shop/orders` | мои покупки |
| `GET` | `/api/shop/orders/{id}` | покупка, купоны, статус доставки |
| `POST` | `/api/shop/orders/{id}/cancel` | отмена покупателем (только `PHYSICAL` в `PAID`) |
| `GET` | `/api/shop/pickup-points` | пункты выдачи |
| `GET` | `/api/me/perks` | активные и будущие привилегии |

Ошибки покупки: `422 insufficient_funds` (как у удержания по заказу), `409 price_changed`,
`409 out_of_stock`, `409 limit_reached`, `409 perk_useless` (ставка уже 0),
`403 verification_required`, `404` для товара, скрытого от роли.

### Админка

| Метод | Путь | Право |
| :--- | :--- | :--- |
| `GET/POST` | `/api/admin/shop/products` | `shop.view` / `shop.create` |
| `PUT` | `/api/admin/shop/products/{id}` | `shop.edit` |
| `POST` | `/api/admin/shop/images` | `shop.edit` — загрузка изображения в `/uploads/shop/` |
| `GET/POST/PUT` | `/api/admin/shop/pickup-points` | `shop.view` / `shop.edit` |
| `GET` | `/api/admin/shop/orders` | `shop_orders.view` — фильтры: статус, товар, период, поиск по номеру и телефону |
| `POST` | `/api/admin/shop/orders/{id}/status` | `shop_orders.edit` — `PROCESSING`/`SHIPPED`/`COMPLETED`, трек |
| `POST` | `/api/admin/shop/orders/{id}/cancel` | `shop_orders.edit` — причина, сумма возврата, вернуть на склад |
| `POST` | `/api/admin/users/{id}/perks` | `shop_orders.edit` — выдать привилегию вручную (компенсация, акция) |
| `DELETE` | `/api/admin/perks/{id}` | `shop_orders.edit` — отозвать без возврата |
| `GET` | `/api/admin/finances/shop` | `shop_revenue.view` — остаток `SHOP`, продажи за период |
| `POST` | `/api/admin/finances/shop/payout` | `shop_revenue.edit` — вывод выручки |

Три раздела прав, а не один: заводить товары (маркетинг), обрабатывать заказы
(склад) и выводить деньги (финансы) — разные люди.

Погашение купона на пункте выдачи — существующий
`POST /api/admin/gifts/coupons/{coupon}/redeem`. Для купона с
`shop_order_id` он же переводит покупку в `COMPLETED`, когда погашены все её
купоны.

---

## 8. Интерфейс

### Покупатель

- Пункт **«Магазин»** в меню заказчика и исполнителя; одна страница в
  `pages/shared/ShopPage.vue` — витрина фильтруется сервером по ролям.
- Витрина: вкладки категорий, карточки с изображением, ценой, «нет в наличии».
- Карточка товара: галерея, описание, варианты, способ получения; для
  привилегии — блок «Сейчас / С привилегией / Окупаемость» (§3.5).
- Окно оформления: итог, баланс до и после, способ получения, адрес с
  подсказками DaData. Кнопка «Оплатить N ₽» блокируется на время запроса;
  `request_id` создаётся при открытии окна и переживает повторное нажатие.
- **«Мои покупки»** — список с номером, статусом, купонами и треком. Купоны
  магазина показываются и на `GiftsPage.vue` рядом с подарками ачивок, с
  пометкой «куплено».
- Плашка активной привилегии на дашборде исполнителя и на странице ачивок, где
  уже показана ставка по уровню: «7 % × 0.5 = 3.5 % до 17 октября».
- Загрузка данных — по правилам [`frontend_data_loading.md`](./frontend_data_loading.md):
  витрина из кэша, скелетоны, фоновая догрузка. Остаток и цена перед оплатой
  всё равно проверяются сервером.

### Админка

- **«Магазин → Товары»** (`admin/ShopProducts.vue`): таблица, форма товара с
  полями по роду, загрузка изображений, привязка к подарку (с показом текущего
  склада / свободных кодов), переключатель активности, предпросмотр карточки.
- **«Магазин → Заказы»** (`admin/ShopOrders.vue`): фильтр по статусу (в адресе,
  как у заказов), карточка покупки со снимком товара, адресом, купонами,
  проводками; кнопки смены статуса и отмены с возвратом. Бейдж с числом `PAID`
  на пункте меню.
- **«Магазин → Пункты выдачи»** — справочник.
- **«Выручка магазина»** (`admin/ShopRevenue.vue`) — по образцу
  `PlatformCommission.vue`: остаток, продажи за период по товарам, вывод.
- В истории пользователя (`UserHistoryModal.vue`) — вкладка «Покупки» и
  активные привилегии.
- Системные настройки: `shop_enabled`.

---

## 9. Открытые вопросы (решает владелец продукта)

1. **Срок и цена привилегии.** 30 дней за 1000 ₽ при базовой ставке 10 %
   окупаются исполнителю с оборотом от 20 000 ₽ в месяц. Кому ниже — невыгодно,
   кому выше — выгодно платформе меньше. Нужны реальные обороты исполнителей,
   чтобы выбрать число; возможно, несколько товаров: 7 / 30 / 90 дней.
2. **Юридическая сторона.** Продажа вещей за деньги — розничная торговля:
   оферта, правила возврата при дистанционной продаже, фискальные чеки.
   Внутренний баланс не освобождает от этого. До открытия мерча нужна
   консультация юриста; привилегии — цифровая услуга, но оферта нужна и им.
3. **Доставка** — включена в цену или отдельной строкой, какие службы.
4. **Остаток по размерам.** В первой версии склад общий на товар, размер —
   пожелание в `fulfillment`. Если размеры кончаются по-разному, каждый размер
   заводится отдельным подарком; полноценные варианты со своим складом — вторая
   версия.
5. **Возврат привилегии** — пропорционально дням или не возвращается совсем.
6. **Покупка баллами ачивок** вместо рублей — отдельная механика, в этот план
   не входит: баллы определяют уровень, и трата баллов снижала бы уровень.

---

## 10. Что вне первой версии

- Корзина из нескольких товаров. Одна покупка — один товар: так проще
  отмены, возвраты и склад, а мерч покупают редко.
- Промокоды и скидки на товары магазина.
- Оплата картой мимо внутреннего баланса.
- Другие виды привилегий (приоритет в выдаче заказов, увеличенный радиус).
  Схема к ним готова — `perk_kind` расширяется, каждый вид получает свою точку
  применения в Go.
