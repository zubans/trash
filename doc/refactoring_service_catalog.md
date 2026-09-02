# Рефакторинг: каталог услуг

## 1. Обоснование

Сейчас типы услуг (`STANDARD`, `LARGE`, `CONSTRUCTION`) и тарифы скорости (`REGULAR`, `URGENT`, `ASAP`, `CUSTOM`) зашиты в код и ENUM PostgreSQL. Добавление нового типа услуги (например, «Выгул собак») требует изменений в БД, бэкенде, фронтенде и локализации.

Цель рефакторинга — вынести каталог услуг в базу данных с иерархической структурой произвольной глубины и дать администратору инструменты для управления категориями и видами услуг.

---

## 2. Текущее состояние

### 2.1 База данных

Файл `backend/migrations/001_create_enums.sql`:

```sql
CREATE TYPE volume_type AS ENUM ('STANDARD', 'LARGE', 'CONSTRUCTION');
CREATE TYPE speed_tariff_type AS ENUM ('REGULAR', 'URGENT', 'ASAP', 'CUSTOM');
```

Таблица `orders` (`backend/migrations/002_create_tables.sql`):

```sql
CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id UUID NOT NULL,
    executor_id UUID,
    volume_type volume_type NOT NULL,
    speed_tariff speed_tariff_type NOT NULL,
    ...
);
```

### 2.2 Бэкенд

Файл `backend/service/order.go`:

```go
func basePriceByVolume(volumeType string) float64 {
    switch volumeType {
    case "STANDARD":
        return 100.0
    case "LARGE":
        return 200.0
    case "CONSTRUCTION":
        return 500.0
    default:
        return 100.0
    }
}
```

Цены базовых типов захардкожены. Тарифы скорости умножаются на коэффициенты из `system_settings`.

### 2.3 Фронтенд

Файл `frontend/src/pages/customer/CustomerDashboard.vue`:

```typescript
const orderTypeOptions = [
  { label: t('customer.types.standardRegular'), value: 'STANDARD_REGULAR', volume_type: 'STANDARD', speed_tariff: 'REGULAR', price: 100 },
  { label: t('customer.types.standardUrgent'), value: 'STANDARD_URGENT', volume_type: 'STANDARD', speed_tariff: 'URGENT', price: 300 },
  ...
]
```

Список типов заказа статичен. Цены дублируются: в коде фронтенда и в коде бэкенда.

### 2.4 Локализация

Файлы `frontend/src/i18n/locales/ru.json` и `en.json`:

```json
"types": {
  "standardRegular": "Стандартный обычный",
  "standardUrgent": "Стандартный срочный",
  ...
}
```

Каждый новый вид услуги требует добавления ключей в JSON.

---

## 3. Целевая архитектура

### 3.1 Выбранная схема: Closure Table

Closure Table позволяет хранить дерево произвольной глубины, быстро получать предков и потомков, и не требует пересчёта целых множеств при перемещении узла.

### 3.2 Сущности

| Сущность | Описание | Пример |
|----------|----------|--------|
| `service_nodes` | Узел каталога: категория или вид услуги | «Вынос мусора», «Один пакет» |
| `service_node_paths` | Замыкания: пары предок-потомок с глубиной | Для быстрого поиска дерева |

### 3.3 Структура таблиц

#### `service_nodes`

```sql
CREATE TABLE IF NOT EXISTS service_nodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_id UUID REFERENCES service_nodes(id),
    code VARCHAR UNIQUE NOT NULL,
    name JSONB NOT NULL,
    description JSONB,
    node_type VARCHAR NOT NULL CHECK (node_type IN ('CATEGORY', 'VARIANT')),
    base_price NUMERIC(18,2),
    is_auction BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    deleted_at TIMESTAMP WITH TIME ZONE NULL,          -- миграция 033
    CONSTRAINT chk_variant_has_price
        CHECK (node_type != 'VARIANT' OR base_price IS NOT NULL),
    CONSTRAINT chk_category_no_price
        CHECK (node_type != 'CATEGORY' OR base_price IS NULL),
    CONSTRAINT chk_deleted_node_inactive                -- миграция 033
        CHECK (deleted_at IS NULL OR is_active = FALSE)
);

-- Миграция 033 снимает UNIQUE с code и заменяет его частичным индексом:
-- код уникален только среди живых узлов, поэтому после удаления его можно
-- занять заново.
CREATE UNIQUE INDEX idx_service_nodes_code_live
    ON service_nodes (code) WHERE deleted_at IS NULL;
```

Поля:

| Поле | Тип | Назначение |
|------|-----|-----------|
| `id` | UUID | Первичный ключ |
| `parent_id` | UUID | Родительский узел. NULL для корневых категорий |
| `code` | VARCHAR | Machine-readable идентификатор. Уникален, неизменен после создания |
| `name` | JSONB | Локализованные названия: `{"ru": "...", "en": "..."}` |
| `description` | JSONB | Локализованные описания |
| `node_type` | VARCHAR | `CATEGORY` или `VARIANT` |
| `base_price` | NUMERIC | Базовая цена вида. Для категории NULL |
| `is_auction` | BOOLEAN | Признак аукционного заказа (ставки исполнителей) |
| `is_active` | BOOLEAN | Видимость в каталоге |
| `sort_order` | INT | Порядок сортировки на одном уровне |
| `created_at` | TIMESTAMPTZ | Дата создания |
| `updated_at` | TIMESTAMPTZ | Дата обновления |
| `deleted_at` | TIMESTAMPTZ | Дата мягкого удаления. NULL — узел живой |

Удаление мягкое, потому что `orders.service_variant_id` ссылается на
`service_nodes` всё время жизни истории заказов: физическое удаление либо
упало бы на внешнем ключе, либо унесло бы историю. Узел с `deleted_at`
одновременно принудительно выключен (`is_active = FALSE`), поэтому старые
запросы вида `is_active = TRUE` уже его не видят, а каталог дополнительно
фильтрует по `deleted_at IS NULL`. Путь узла в `service_node_paths`
сохраняется — заказ на удалённую услугу по-прежнему показывает её категорию.

#### `service_node_paths`

```sql
CREATE TABLE IF NOT EXISTS service_node_paths (
    ancestor_id UUID REFERENCES service_nodes(id) ON DELETE CASCADE,
    descendant_id UUID REFERENCES service_nodes(id) ON DELETE CASCADE,
    depth INT NOT NULL,
    PRIMARY KEY (ancestor_id, descendant_id)
);

CREATE INDEX idx_service_node_paths_ancestor ON service_node_paths(ancestor_id);
CREATE INDEX idx_service_node_paths_descendant ON service_node_paths(descendant_id);
```

Поля:

| Поле | Тип | Назначение |
|------|-----|-----------|
| `ancestor_id` | UUID | ID предка |
| `descendant_id` | UUID | ID потомка |
| `depth` | INT | Глубина от предка до потомка. 0 — сам узел |

#### Обновлённая таблица `orders`

```sql
ALTER TABLE orders
    ADD COLUMN service_variant_id UUID REFERENCES service_nodes(id),
    ADD COLUMN is_urgent BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN is_asap BOOLEAN NOT NULL DEFAULT FALSE;
```

После миграции данных:

```sql
ALTER TABLE orders
    ALTER COLUMN service_variant_id SET NOT NULL;

ALTER TABLE orders
    DROP COLUMN volume_type,
    DROP COLUMN speed_tariff;

DROP TYPE IF EXISTS volume_type;
```

> `speed_tariff_type` остаётся, если используется в других местах. Если нет — удалить.

#### `mobile_app_releases`

```sql
CREATE TABLE IF NOT EXISTS mobile_app_releases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    platform VARCHAR NOT NULL CHECK (platform IN ('android')),
    version_name VARCHAR NOT NULL,
    version_code INT NOT NULL,
    file_name VARCHAR NOT NULL,
    file_path VARCHAR NOT NULL,
    release_notes TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    force_update BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_mobile_app_releases_code
    ON mobile_app_releases(platform, version_code);

CREATE INDEX idx_mobile_app_releases_active
    ON mobile_app_releases(platform, is_active)
    WHERE is_active = TRUE;
```

Поля:

| Поле | Тип | Назначение |
|------|-----|-----------|
| `id` | UUID | Первичный ключ |
| `platform` | VARCHAR | Платформа (`android`) |
| `version_name` | VARCHAR | Версия для отображения (например, `1.2.0`) |
| `version_code` | INT | Числовой код версии для сравнения |
| `file_name` | VARCHAR | Имя файла APK |
| `file_path` | VARCHAR | Относительный путь к файлу, например `/releases/android/app-release.apk` |
| `release_notes` | TEXT | Описание изменений |
| `is_active` | BOOLEAN | Активен ли релиз |
| `force_update` | BOOLEAN | Принудительное обновление |
| `created_at` | TIMESTAMPTZ | Дата создания |

### 3.4 Начальное наполнение

```sql
INSERT INTO service_nodes (id, parent_id, code, name, description, node_type, base_price, is_auction, is_active, sort_order) VALUES
-- Корневая категория: Вынос мусора
('11111111-1111-1111-1111-111111111111', NULL, 'trash_removal',
 '{"ru": "Вынос мусора", "en": "Trash removal"}',
 '{"ru": "Вывоз бытовых отходов", "en": "Household waste removal"}',
 'CATEGORY', NULL, FALSE, TRUE, 1),

-- Подкатегория: Стандартный
('22222222-2222-2222-2222-222222222222', '11111111-1111-1111-1111-111111111111', 'trash_standard',
 '{"ru": "Стандартный", "en": "Standard"}',
 '{"ru": "Стандартный объём", "en": "Standard volume"}',
 'CATEGORY', NULL, FALSE, TRUE, 1),

-- Вид: Один пакет
('33333333-3333-3333-3333-333333333333', '22222222-2222-2222-2222-222222222222', 'trash_standard_single',
 '{"ru": "Один пакет", "en": "Single bag"}',
 '{"ru": "До 30 литров", "en": "Up to 30 liters"}',
 'VARIANT', 100.00, FALSE, TRUE, 1),

-- Вид: Двойной пакет
('44444444-4444-4444-4444-444444444444', '22222222-2222-2222-2222-222222222222', 'trash_standard_double',
 '{"ru": "Двойной пакет", "en": "Double bag"}',
 '{"ru": "До 60 литров", "en": "Up to 60 liters"}',
 'VARIANT', 180.00, FALSE, TRUE, 2),

-- Подкатегория: Большой
('55555555-5555-5555-5555-555555555555', '11111111-1111-1111-1111-111111111111', 'trash_large',
 '{"ru": "Большой", "en": "Large"}',
 '{"ru": "Крупногабаритный мусор", "en": "Bulky waste"}',
 'CATEGORY', NULL, FALSE, TRUE, 2),

-- Вид: Большой стандартный
('66666666-6666-6666-6666-666666666666', '55555555-5555-5555-5555-555555555555', 'trash_large_regular',
 '{"ru": "Большой обычный", "en": "Large regular"}',
 NULL, 'VARIANT', 200.00, FALSE, TRUE, 1),

-- Вид: Строительный мусор (аукцион)
('77777777-7777-7777-7777-777777777777', '11111111-1111-1111-1111-111111111111', 'trash_construction',
 '{"ru": "Строительный мусор", "en": "Construction waste"}',
 '{"ru": "Аукцион исполнителей", "en": "Executor auction"}',
 'VARIANT', 0.00, TRUE, TRUE, 3),

-- Корневая категория: Выгул собак
('88888888-8888-8888-8888-888888888888', NULL, 'dog_walking',
 '{"ru": "Выгул собак", "en": "Dog walking"}',
 NULL, 'CATEGORY', NULL, FALSE, TRUE, 2),

-- Вид: Утренний выгул
('99999999-9999-9999-9999-999999999999', '88888888-8888-8888-8888-888888888888', 'dog_walk_morning',
 '{"ru": "Утренний выгул", "en": "Morning walk"}',
 NULL, 'VARIANT', 150.00, FALSE, TRUE, 1),

-- Вид: Вечерний выгул
('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', '88888888-8888-8888-8888-888888888888', 'dog_walk_evening',
 '{"ru": "Вечерний выгул", "en": "Evening walk"}',
 NULL, 'VARIANT', 150.00, FALSE, TRUE, 2),

-- Вид: Передержка
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', '88888888-8888-8888-8888-888888888888', 'dog_overnight',
 '{"ru": "Передержка", "en": "Overnight stay"}',
 NULL, 'VARIANT', 800.00, FALSE, TRUE, 3);
```

### 3.5 Заполнение замыканий

Каждый узел должен получить записи о пути к себе и ко всем своим предкам. Для seed-данных это делается явно:

```sql
INSERT INTO service_node_paths (ancestor_id, descendant_id, depth) VALUES
-- trash_removal
('11111111-1111-1111-1111-111111111111', '11111111-1111-1111-1111-111111111111', 0),
('11111111-1111-1111-1111-111111111111', '22222222-2222-2222-2222-222222222222', 1),
('11111111-1111-1111-1111-111111111111', '33333333-3333-3333-3333-333333333333', 2),
('11111111-1111-1111-1111-111111111111', '44444444-4444-4444-4444-444444444444', 2),
('11111111-1111-1111-1111-111111111111', '55555555-5555-5555-5555-555555555555', 1),
('11111111-1111-1111-1111-111111111111', '66666666-6666-6666-6666-666666666666', 2),
('11111111-1111-1111-1111-111111111111', '77777777-7777-7777-7777-777777777777', 1),

-- trash_standard
('22222222-2222-2222-2222-222222222222', '22222222-2222-2222-2222-222222222222', 0),
('22222222-2222-2222-2222-222222222222', '33333333-3333-3333-3333-333333333333', 1),
('22222222-2222-2222-2222-222222222222', '44444444-4444-4444-4444-444444444444', 1),

-- trash_standard_single
('33333333-3333-3333-3333-333333333333', '33333333-3333-3333-3333-333333333333', 0),

-- и так далее для всех узлов
;
```

Для динамического создания/перемещения узлов в админке нужна хранимая функция:

```sql
CREATE OR REPLACE FUNCTION rebuild_service_node_paths(p_node_id UUID)
RETURNS VOID AS $$
BEGIN
    -- 1. Удаляем все старые пути для узла и его поддерева.
    --    Без этого при перемещении узла потомки сохранят пути через прежних предков.
    DELETE FROM service_node_paths
    WHERE descendant_id IN (
        SELECT id FROM service_nodes WHERE id = p_node_id
        UNION ALL
        WITH RECURSIVE descendants AS (
            SELECT id FROM service_nodes WHERE parent_id = p_node_id
            UNION ALL
            SELECT sn.id FROM service_nodes sn
            JOIN descendants d ON sn.parent_id = d.id
        )
        SELECT id FROM descendants
    );

    -- 2. Путь узла к самому себе (depth 0).
    INSERT INTO service_node_paths (ancestor_id, descendant_id, depth)
    VALUES (p_node_id, p_node_id, 0);

    -- 3. Пути от всех предков узла к самому узлу.
    INSERT INTO service_node_paths (ancestor_id, descendant_id, depth)
    SELECT sp.ancestor_id, p_node_id, sp.depth + 1
    FROM service_node_paths sp
    WHERE sp.descendant_id = (SELECT parent_id FROM service_nodes WHERE id = p_node_id);

    -- 4. Пути от самого узла ко всем его потомкам.
    WITH RECURSIVE descendants AS (
        SELECT id, parent_id, 1 AS depth
        FROM service_nodes
        WHERE parent_id = p_node_id
        UNION ALL
        SELECT sn.id, sn.parent_id, d.depth + 1
        FROM service_nodes sn
        JOIN descendants d ON sn.parent_id = d.id
    )
    INSERT INTO service_node_paths (ancestor_id, descendant_id, depth)
    SELECT p_node_id, d.id, d.depth
    FROM descendants d;

    -- 5. Пути от предков узла (кроме него самого) ко всем его потомкам.
    WITH RECURSIVE descendants AS (
        SELECT id, parent_id, 1 AS depth
        FROM service_nodes
        WHERE parent_id = p_node_id
        UNION ALL
        SELECT sn.id, sn.parent_id, d.depth + 1
        FROM service_nodes sn
        JOIN descendants d ON sn.parent_id = d.id
    )
    INSERT INTO service_node_paths (ancestor_id, descendant_id, depth)
    SELECT a.ancestor_id, d.id, a.depth + d.depth
    FROM descendants d
    JOIN (
        SELECT ancestor_id, depth
        FROM service_node_paths
        WHERE descendant_id = p_node_id AND ancestor_id <> p_node_id
    ) a ON TRUE;
END;
$$ LANGUAGE plpgsql;
```

> **Примечание:** функция `rebuild_service_node_paths` является допущением **H1**. Возможна реализация полного пересчёта путей внутри транзакции приложения, но хранимая функция надёжнее. При вызове для seed-данных узлы следует обходить сверху вниз (корни раньше потомков), чтобы каждый последующий вызов не пересчитывал уже построенные пути родителей.
>
> **Важно:** функция должна вызываться внутри транзакции приложения (`CreateNode`, `UpdateNode`).

---

## 4. Маппинг старых данных

### 4.1 Таблица соответствия

| Старое значение `volume_type` + `speed_tariff` | Новый `service_variant_id` | Флаги срочности | Комментарий |
|-----------------------------------------------|---------------------------|------------------|-------------|
| `STANDARD` + `REGULAR` | `33333333-3333-3333-3333-333333333333` (`trash_standard_single`) | `is_urgent = FALSE`, `is_asap = FALSE` | Обычный заказ |
| `STANDARD` + `URGENT` | `33333333-3333-3333-3333-333333333333` (`trash_standard_single`) | `is_urgent = TRUE`, `is_asap = FALSE` | Срочный × 3 |
| `STANDARD` + `ASAP` | `33333333-3333-3333-3333-333333333333` (`trash_standard_single`) | `is_urgent = FALSE`, `is_asap = TRUE` | ASAP × 8 |
| `LARGE` + `REGULAR` | `66666666-6666-6666-6666-666666666666` (`trash_large_regular`) | `is_urgent = FALSE`, `is_asap = FALSE` | Большой обычный |
| `LARGE` + `URGENT` | `66666666-6666-6666-6666-666666666666` (`trash_large_regular`) | `is_urgent = TRUE`, `is_asap = FALSE` | Большой срочный |
| `LARGE` + `ASAP` | `66666666-6666-6666-6666-666666666666` (`trash_large_regular`) | `is_urgent = FALSE`, `is_asap = TRUE` | Большой ASAP |
| `CONSTRUCTION` + `CUSTOM` | `77777777-7777-7777-7777-777777777777` (`trash_construction`) | `is_urgent = FALSE`, `is_asap = FALSE` | Аукцион |

### 4.2 Миграционный SQL

```sql
-- Сначала добавляем новые колонки
ALTER TABLE orders
    ADD COLUMN service_variant_id UUID REFERENCES service_nodes(id),
    ADD COLUMN is_urgent BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN is_asap BOOLEAN NOT NULL DEFAULT FALSE;

-- Заполняем service_variant_id, is_urgent и is_asap.
-- Если таблица orders большая, этот UPDATE следует заменить на батчевый скрипт
-- (см. раздел 7.1) или выполнять в окно обслуживания.
UPDATE orders
SET
    service_variant_id = CASE
        WHEN volume_type = 'STANDARD' THEN '33333333-3333-3333-3333-333333333333'::UUID
        WHEN volume_type = 'LARGE' THEN '66666666-6666-6666-6666-666666666666'::UUID
        WHEN volume_type = 'CONSTRUCTION' THEN '77777777-7777-7777-7777-777777777777'::UUID
    END,
    is_urgent = CASE WHEN speed_tariff = 'URGENT' THEN TRUE ELSE FALSE END,
    is_asap = CASE WHEN speed_tariff = 'ASAP' THEN TRUE ELSE FALSE END;

-- Проверяем, что не осталось строк без маппинга
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM orders WHERE service_variant_id IS NULL) THEN
        RAISE EXCEPTION 'Found orders without service_variant_id mapping';
    END IF;
END $$;

-- Делаем NOT NULL после заполнения
ALTER TABLE orders
    ALTER COLUMN service_variant_id SET NOT NULL;

-- Удаляем старые колонки и ENUM
ALTER TABLE orders
    DROP COLUMN volume_type,
    DROP COLUMN speed_tariff;

DROP TYPE IF EXISTS volume_type;
```

> **Важно:** перед выполнением миграции на продакшене необходимо сделать резервную копию БД. Если таблица `orders` содержит миллионы записей, UPDATE следует выполнять батчами или в окно обслуживания.
>
> **Проверка `speed_tariff_type`:** перед `DROP TYPE volume_type` убедиться, что этот ENUM не используется в других таблицах/функциях. `speed_tariff_type` удаляется отдельно, если больше не используется.

---

## 5. Бэкенд

### 5.1 Новые модели

Файл `backend/repository/service_catalog.go`:

```go
package repository

import (
    "database/sql"
    "encoding/json"
    "time"

    "github.com/google/uuid"
)

// ServiceNodeType определяет тип узла каталога.
type ServiceNodeType string

const (
    ServiceNodeTypeCategory ServiceNodeType = "CATEGORY"
    ServiceNodeTypeVariant  ServiceNodeType = "VARIANT"
)

// LocalizedText хранит переводы для одного поля.
type LocalizedText map[string]string

// ServiceNode represents a node in the service catalog tree.
type ServiceNode struct {
    ID          uuid.UUID       `json:"id"`
    ParentID    *uuid.UUID      `json:"parent_id,omitempty"`
    Code        string          `json:"code"`
    Name        LocalizedText   `json:"name"`
    Description LocalizedText   `json:"description,omitempty"`
    NodeType    ServiceNodeType `json:"node_type"`
    BasePrice   *float64        `json:"base_price,omitempty"`
    IsAuction   bool            `json:"is_auction"`
    IsActive    bool            `json:"is_active"`
    SortOrder   int             `json:"sort_order"`
    CreatedAt   time.Time       `json:"created_at"`
    UpdatedAt   time.Time       `json:"updated_at"`
}

// IsCategory возвращает true, если узел — категория.
func (n *ServiceNode) IsCategory() bool {
    return n.NodeType == ServiceNodeTypeCategory
}

// IsVariant возвращает true, если узел — вид услуги.
func (n *ServiceNode) IsVariant() bool {
    return n.NodeType == ServiceNodeTypeVariant
}
```

### 5.2 Интерфейс репозитория

```go
// ServiceCatalogRepository defines storage operations for the service catalog.
type ServiceCatalogRepository interface {
    // CRUD
    CreateNode(node *ServiceNode) error
    UpdateNode(node *ServiceNode) error
    DeleteNode(id uuid.UUID) error
    GetNodeByID(id uuid.UUID) (*ServiceNode, error)
    GetNodeByCode(code string) (*ServiceNode, error)

    // Tree navigation
    GetRootCategories(activeOnly bool) ([]*ServiceNode, error)
    GetChildren(parentID uuid.UUID, activeOnly bool) ([]*ServiceNode, error)
    GetDescendants(ancestorID uuid.UUID, maxDepth *int) ([]*ServiceNode, error)
    GetAncestors(descendantID uuid.UUID) ([]*ServiceNode, error)
    GetVariantPath(variantID uuid.UUID) ([]*ServiceNode, error)

    // Catalog helpers
    GetActiveVariants() ([]*ServiceNode, error)
    GetVariantWithCategory(id uuid.UUID) (*ServiceNode, []*ServiceNode, error)
}

type serviceCatalogRepo struct {
    db *sql.DB
}

// NewServiceCatalogRepository creates a new service catalog repository.
func NewServiceCatalogRepository(db *sql.DB) ServiceCatalogRepository {
    return &serviceCatalogRepo{db: db}
}
```

### 5.3 Реализация ключевых методов

#### CreateNode

```go
func (r *serviceCatalogRepo) CreateNode(node *ServiceNode) error {
    if node.ID == uuid.Nil {
        node.ID = uuid.New()
    }
    now := time.Now()
    node.CreatedAt = now
    node.UpdatedAt = now

    nameJSON, err := json.Marshal(node.Name)
    if err != nil {
        return err
    }

    descJSON, err := json.Marshal(node.Description)
    if err != nil {
        return err
    }

    tx, err := r.db.Begin()
    if err != nil {
        return err
    }
    defer tx.Rollback()

    query := `
        INSERT INTO service_nodes (id, parent_id, code, name, description, node_type, base_price, is_auction, is_active, sort_order, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
    `
    _, err = tx.Exec(query,
        node.ID, node.ParentID, node.Code, nameJSON, descJSON,
        node.NodeType, node.BasePrice, node.IsAuction, node.IsActive, node.SortOrder, node.CreatedAt, node.UpdatedAt,
    )
    if err != nil {
        return err
    }

    _, err = tx.Exec(`SELECT rebuild_service_node_paths($1)`, node.ID)
    if err != nil {
        return err
    }

    return tx.Commit()
}
```

#### UpdateNode

```go
func (r *serviceCatalogRepo) UpdateNode(node *ServiceNode) error {
    node.UpdatedAt = time.Now()

    nameJSON, err := json.Marshal(node.Name)
    if err != nil {
        return err
    }

    descJSON, err := json.Marshal(node.Description)
    if err != nil {
        return err
    }

    // Запрещаем назначать родителем самого себя или любого из потомков —
    // иначе дерево замкнётся в цикл.
    if node.ParentID != nil {
        isDescendant, err := r.isDescendantOf(*node.ParentID, node.ID)
        if err != nil {
            return err
        }
        if node.ID == *node.ParentID || isDescendant {
            return errors.New("cannot set parent to self or descendant")
        }
    }

    tx, err := r.db.Begin()
    if err != nil {
        return err
    }
    defer tx.Rollback()

    query := `
        UPDATE service_nodes
        SET parent_id = $2, name = $3, description = $4, base_price = $5,
            is_auction = $6, is_active = $7, sort_order = $8, updated_at = $9
        WHERE id = $1
    `
    _, err = tx.Exec(query,
        node.ID, node.ParentID, nameJSON, descJSON,
        node.BasePrice, node.IsAuction, node.IsActive, node.SortOrder, node.UpdatedAt,
    )
    if err != nil {
        return err
    }

    // Пересчитываем пути в любом случае: parent_id мог измениться,
    // либо имя/цена не влияют на пути, но вызов идемпотентен.
    _, err = tx.Exec(`SELECT rebuild_service_node_paths($1)`, node.ID)
    if err != nil {
        return err
    }

    return tx.Commit()
}

// isDescendantOf возвращает true, если candidateDescendant находится в поддереве candidateAncestor.
func (r *serviceCatalogRepo) isDescendantOf(candidateAncestor, candidateDescendant uuid.UUID) (bool, error) {
    var exists bool
    err := r.db.QueryRow(`
        SELECT EXISTS(
            SELECT 1 FROM service_node_paths
            WHERE ancestor_id = $1 AND descendant_id = $2 AND depth > 0
        )
    `, candidateAncestor, candidateDescendant).Scan(&exists)
    return exists, err
}
```

> **Примечание:** `code` и `node_type` не редактируются после создания. Это ограничение **H2**.

#### GetRootCategories

```sql
SELECT id, parent_id, code, name, description, node_type, base_price, is_auction, is_active, sort_order, created_at, updated_at
FROM service_nodes
WHERE parent_id IS NULL
ORDER BY sort_order, name->>'ru';
```

#### GetChildren

```sql
SELECT sn.id, sn.parent_id, sn.code, sn.name, sn.description, sn.node_type, sn.base_price, sn.is_auction, sn.is_active, sn.sort_order, sn.created_at, sn.updated_at
FROM service_nodes sn
WHERE sn.parent_id = $1
ORDER BY sn.sort_order, sn.name->>'ru';
```

#### GetVariantPath

```sql
SELECT sn.id, sn.parent_id, sn.code, sn.name, sn.description, sn.node_type, sn.base_price, sn.is_auction, sn.is_active, sn.sort_order, sn.created_at, sn.updated_at
FROM service_node_paths p
JOIN service_nodes sn ON sn.id = p.ancestor_id
WHERE p.descendant_id = $1
ORDER BY p.depth ASC;
```

### 5.4 Обновление модели Order

Файл `backend/repository/order.go`:

```go
type Order struct {
    ID               uuid.UUID    `json:"id"`
    CustomerID       uuid.UUID    `json:"customer_id"`
    ExecutorID       *uuid.UUID   `json:"executor_id,omitempty"`
    ServiceVariantID uuid.UUID    `json:"service_variant_id"`
    IsUrgent         bool         `json:"is_urgent"`
    IsAsap           bool         `json:"is_asap"`
    Status           OrderStatus  `json:"status"`
    HoldAmount       float64      `json:"hold_amount"`
    FinalAmount      float64      `json:"final_amount"`
    IsDowngraded     bool         `json:"is_downgraded"`
    PhotoURL         *string      `json:"photo_url,omitempty"`
    Address          *string      `json:"address,omitempty"`
    PickupLat        *float64     `json:"pickup_lat,omitempty"`
    PickupLon        *float64     `json:"pickup_lon,omitempty"`
    CreatedAt        time.Time    `json:"created_at"`
    AssignedAt       *time.Time   `json:"assigned_at,omitempty"`
    DeadlineAt       *time.Time   `json:"deadline_at,omitempty"`
    CompletedAt      *time.Time   `json:"completed_at,omitempty"`
    CanceledAt       *time.Time   `json:"canceled_at,omitempty"`
}
```

### 5.5 Обновление OrderRepository

Все SQL-запросы в `backend/repository/order.go`, `backend/repository/admin.go`, `backend/repository/bid.go` нужно перевести с `volume_type`/`speed_tariff` на `service_variant_id`, `is_urgent` и `is_asap`.

Пример `Create`:

```go
func (r *orderRepo) Create(order *Order) error {
    _, err := r.db.Exec(
        `INSERT INTO orders (id, customer_id, executor_id, service_variant_id, is_urgent, is_asap, status, hold_amount, final_amount, is_downgraded, photo_url, address, pickup_lat, pickup_lon, created_at, deadline_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
        order.ID, order.CustomerID, order.ExecutorID, order.ServiceVariantID, order.IsUrgent, order.IsAsap,
        order.Status, order.HoldAmount, order.FinalAmount, order.IsDowngraded, order.PhotoURL,
        order.Address, order.PickupLat, order.PickupLon,
        order.CreatedAt, order.DeadlineAt,
    )
    return err
}
```

### 5.6 Обновление ценообразования

Файл `backend/service/order.go`:

```go
func (s *OrderService) CalculatePrice(serviceVariantID uuid.UUID, isUrgent, isAsap bool, isDowngraded bool) (float64, error) {
    variant, err := s.catalogRepo.GetNodeByID(serviceVariantID)
    if err != nil {
        return 0, err
    }
    if variant == nil || !variant.IsVariant() {
        return 0, errors.New("invalid service variant")
    }
    if variant.BasePrice == nil {
        return 0, errors.New("variant has no base price")
    }

    price := *variant.BasePrice

    if variant.IsAuction {
        return 0, nil
    }

    if isDowngraded {
        return price, nil
    }

    settings := s.loadSettings()
    switch {
    case isAsap:
        price *= settings["asap_tariff_coeff"]
    case isUrgent:
        price *= settings["urgent_tariff_coeff"]
    }

    return price, nil
}
```

> **Допущение H3:** динамические модификаторы цены — два уровня срочности (`is_urgent` и `is_asap`). Коэффициенты `urgent_tariff_coeff` (по умолчанию 3.0) и `asap_tariff_coeff` (по умолчанию 8.0) хранятся в `system_settings`.
>
> **Downgrade ASAP:** если заказ с `is_asap = true` не подтверждён до истечения 15-минутного SLA, цена даунгрейдится до базовой (`is_downgraded = true`).
>
> **Важно:** для аукционных вариантов (`is_auction = true`) расчётная цена всегда 0; окончательная цена формируется по ставкам исполнителей.

### 5.7 Обновление CreateOrderRequest

```go
type CreateOrderRequest struct {
    ServiceVariantID uuid.UUID `json:"service_variant_id"`
    IsUrgent         bool      `json:"is_urgent"`
    IsAsap           bool      `json:"is_asap"`
    PhotoURL         *string   `json:"photo_url,omitempty"`
    Address          string    `json:"address"`
    Lat              *float64  `json:"lat,omitempty"`
    Lon              *float64  `json:"lon,omitempty"`
}
```

### 5.8 Аукционы

В `backend/repository/bid.go` и `backend/service/order.go` заменить:

```go
if volumeType != "CONSTRUCTION" {
    return nil, errors.New("cannot bid on non-construction orders")
}
```

на:

```go
variant, err := s.catalogRepo.GetNodeByID(order.ServiceVariantID)
if err != nil || variant == nil || !variant.IsAuction {
    return nil, errors.New("cannot bid on non-auction orders")
}
```

Для списка доступных аукционных заказов (`/executor/orders/available`) фильтр по `volume_type = 'CONSTRUCTION'` заменить на фильтр по `is_auction = TRUE` в `service_nodes`:

```sql
SELECT o.*
FROM orders o
JOIN service_nodes sn ON sn.id = o.service_variant_id
WHERE sn.is_auction = TRUE
  AND o.status = 'SEARCHING';
```

### 5.9 API-эндпоинты

#### Публичные

| Метод | Эндпоинт | Описание |
|-------|----------|----------|
| GET | `/service-categories` | Корневые категории (active) |
| GET | `/service-categories/:id/children` | Дочерние узлы категории |
| GET | `/service-categories/:id/variants` | Все виды в категории (любой глубины) |
| GET | `/service-variants` | Плоский список активных видов |
| GET | `/service-variants/:id` | Детали вида |

#### Административные

| Метод | Эндпоинт | Описание |
|-------|----------|----------|
| GET | `/admin/service-nodes` | Дерево всех узлов |
| GET | `/admin/service-nodes/:id` | Детали узла |
| POST | `/admin/service-nodes` | Создать узел |
| PUT | `/admin/service-nodes/:id` | Обновить узел |
| DELETE | `/admin/service-nodes/:id` | Мягко удалить узел (строка остаётся, заказы сохраняются) |
| POST | `/admin/service-nodes/:id/restore` | Восстановить удалённый узел (возвращается выключенным) |

`GET /admin/service-nodes` по умолчанию не отдаёт удалённые узлы; чтобы увидеть
их в дереве, нужен параметр `?include_deleted=true`.

Ответ `DELETE` содержит `{"message": ..., "soft": true, "had_orders": bool, "deleted_count": N}` —
`had_orders` говорит админке, что за услугой осталась история заказов, а
`deleted_count` — сколько узлов ушло вместе с этим (узел + всё поддерево).
Удаление **каскадное**: снимая категорию, гасим и всех её живых потомков.

### 5.10 Валидация в админских эндпоинтах

- `code` уникален, совпадает с `^[a-z0-9_]+$`.
- `node_type` не меняется после создания.
- `VARIANT` может быть только листом: у него не должно быть дочерних узлов.
- `CATEGORY` не может иметь `base_price`; `base_price: 0` от общей формы
  трактуется как «цены нет», а не как конфликт.
- `node_type` и `code` на обновлении берутся из сохранённого узла, а не из тела
  запроса: клиент их не присылает, потому что менять их нельзя.
- Нельзя назначить родителем сам узел или любого из его потомков (защита от циклов).
- Удаление каскадное: удаляя категорию, мягко гасим её вместе со всем живым
  поддеревом одним оператором (рекурсивный обход по `parent_id`, не зависящий от
  closure-таблицы). Уже удалённые узлы пропускаются по `deleted_at`.
- Вид с заказами удалить можно: удаление мягкое, строка остаётся на месте, и
  `orders.service_variant_id` продолжает разрешаться.
- Удалённый узел нельзя редактировать и нельзя назначить родителем — сначала
  восстановление, потом правки. Восстановление **не** каскадное: возвращается
  один узел (выключенным); вложенные восстанавливаются сверху вниз по мере
  надобности.
- `name` должен содержать хотя бы ключ по умолчанию (`ru`) и может содержать дополнительные языки (например, `en`).
- Для аукционного варианта `base_price` должен быть `0` (или `NULL`, если изменить констрейнт `chk_variant_has_price`).

### 5.11 Релизы мобильного приложения

#### Модель

Файл `backend/repository/app_release.go`:

```go
package repository

import (
    "database/sql"
    "time"

    "github.com/google/uuid"
)

type AppRelease struct {
    ID           uuid.UUID `json:"id"`
    Platform     string    `json:"platform"`
    VersionName  string    `json:"version_name"`
    VersionCode  int       `json:"version_code"`
    FileName     string    `json:"file_name"`
    FilePath     string    `json:"file_path"`
    ReleaseNotes string    `json:"release_notes,omitempty"`
    IsActive     bool      `json:"is_active"`
    ForceUpdate  bool      `json:"force_update"`
    CreatedAt    time.Time `json:"created_at"`
}

type AppReleaseRepository interface {
    GetActiveRelease(platform string) (*AppRelease, error)
    CreateRelease(release *AppRelease) error
    DeactivateOldReleases(platform string, excludeID uuid.UUID) error
}
```

#### API-эндпоинты

| Метод | Эндпоинт | Доступ | Описание |
|-------|----------|--------|----------|
| GET | `/app/version?platform=android` | Публичный | Текущий активный релиз |
| POST | `/admin/app-releases` | Admin | Загрузить новый APK и активировать релиз |

Ответ `GET /app/version`:

```json
{
  "version_name": "1.2.0",
  "version_code": 3,
  "download_url": "https://api.example.com/releases/android/app-release.apk",
  "force_update": false,
  "release_notes": "Исправлены баги и добавлен каталог услуг"
}
```

#### Логика загрузки APK

`backend/handler/app_release.go`:

```go
func (h *AppReleaseHandler) UploadReleaseHandler(w http.ResponseWriter, r *http.Request) {
    platform := r.FormValue("platform")
    versionName := r.FormValue("version_name")
    versionCode, _ := strconv.Atoi(r.FormValue("version_code"))

    file, header, err := r.FormFile("apk")
    if err != nil {
        http.Error(w, "apk file required", http.StatusBadRequest)
        return
    }
    defer file.Close()

    // Сохраняем файл в releases/<platform>/
    fileName := fmt.Sprintf("app-release-%s-%d.apk", versionName, versionCode)
    filePath := filepath.Join("releases", platform, fileName)
    fullPath := filepath.Join(h.releasesDir, filePath)

    if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    out, err := os.Create(fullPath)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    defer out.Close()

    if _, err := io.Copy(out, file); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    release := &repository.AppRelease{
        ID:          uuid.New(),
        Platform:    platform,
        VersionName: versionName,
        VersionCode: versionCode,
        FileName:    fileName,
        FilePath:    "/" + filepath.ToSlash(filePath),
        IsActive:    true,
    }

    if err := h.releaseRepo.CreateRelease(release); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(release)
}
```

> **Примечание:** публичный endpoint `/app/version` не должен требовать авторизации, чтобы приложение могло проверять версию до входа в систему.

#### Конфигурация nginx

Добавить `/app/version` в проксируемые пути и раздать статику `/releases/`:

```nginx
location ~ ^/(health|login|register|logout|settings|geo|customer|executor|admin|chats|app)(/.*)?$ {
    proxy_pass http://backend;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}

location /releases/ {
    alias /app/releases/;
    add_header Content-Type application/vnd.android.package-archive;
}
```

> Путь `/app/releases/` в контейнере должен быть смонтирован из хоста или volume. В `docker-compose.yml` добавить:
> ```yaml
> volumes:
>   - ./releases:/app/releases
> ```
>
> Для development `RELEASES_DIR` по умолчанию равен `releases` (относительно рабочей директории backend). В Docker `RELEASES_DIR=/app/releases`.

---

## 6. Фронтенд

### 6.1 API-клиент

Файл `frontend/src/api/services.ts`:

```typescript
import { api } from './index'

export interface ServiceNode {
  id: string
  parent_id?: string
  code: string
  name: Record<string, string>
  description?: Record<string, string>
  node_type: 'CATEGORY' | 'VARIANT'
  base_price?: number
  is_auction: boolean
  is_active: boolean
  sort_order: number
}

export async function getServiceCategories(): Promise<ServiceNode[]> {
  const response = await api.get('/service-categories')
  return response.data
}

export async function getServiceVariants(): Promise<ServiceNode[]> {
  const response = await api.get('/service-variants')
  return response.data
}

export async function getCategoryVariants(categoryId: string): Promise<ServiceNode[]> {
  const response = await api.get(`/service-categories/${categoryId}/variants`)
  return response.data
}
```

### 6.2 Замена orderTypeOptions

Файл `frontend/src/pages/customer/CustomerDashboard.vue`.

Удалить:

```typescript
const orderTypeOptions = [
  { label: t('customer.types.standardRegular'), value: 'STANDARD_REGULAR', volume_type: 'STANDARD', speed_tariff: 'REGULAR', price: 100 },
  ...
]
```

Добавить динамическую загрузку:

```typescript
import { getServiceCategories, getCategoryVariants } from '@/api/services'
import { useI18n } from 'vue-i18n'

const { locale } = useI18n()

const serviceCategories = ref<ServiceNode[]>([])
const serviceVariants = ref<ServiceNode[]>([])
const selectedCategoryId = ref<string | null>(null)
const selectedVariantId = ref<string | null>(null)
const isUrgent = ref(false)
const isAsap = ref(false)
const selectedVariant = computed(() =>
  serviceVariants.value.find(v => v.id === selectedVariantId.value)
)

const localizedName = (node?: ServiceNode) =>
  node?.name[locale.value] || node?.name['ru'] || node?.code || ''

const categoryOptions = computed(() =>
  serviceCategories.value.map(c => ({ label: localizedName(c), value: c.id }))
)

const variantOptions = computed(() =>
  serviceVariants.value.map(v => ({ label: localizedName(v), value: v.id }))
)

// Для аукционных вариантов флаги срочности не применимы
const isAuctionSelected = computed(() => selectedVariant.value?.is_auction)

onMounted(async () => {
  serviceCategories.value = await getServiceCategories()
})

watch(selectedCategoryId, async (id) => {
  if (!id) {
    serviceVariants.value = []
    return
  }
  serviceVariants.value = await getCategoryVariants(id)
  selectedVariantId.value = null
})

watch(selectedVariantId, () => {
  // Сбрасываем срочность при смене вида, чтобы не остался некорректный комбинация
  isUrgent.value = false
  isAsap.value = false
})
```

> **Примечание:** текущий seed-каталог содержит подкатегории (`trash_standard`, `trash_large`). Если требуется показывать пользователю промежуточный уровень, реализовать каскадный выбор: категория → подкатегория → вид. Для MVP можно загружать виды любой глубины через `getCategoryVariants`, но тогда пользователь не увидит разделение «Стандартный / Большой».

#### 6.2a Навигация по смешанному уровню (актуальная реализация)

Фиксированная лестница «категория → подкатегория → вид» из примера выше не
подходит, когда на одном уровне рядом лежат и подкатегория, и услуга: старая
витрина в этом случае показывала только категории, а услуги того же уровня
пропадали. Компонент [`CustomerDashboardV2.vue`](../frontend/src/pages/customer/CustomerDashboardV2.vue)
вместе с [`components/CreateOrderModal.vue`](../frontend/src/pages/customer/components/CreateOrderModal.vue)
обходит дерево **по одному уровню за раз**, не делая предположений о его глубине:

- на каждом уровне показываются **все** дочерние узлы — и категории, и услуги,
  вперемешку, в порядке `sort_order`;
- нажатие на **категорию** проваливается на её дочерний уровень (загружается
  через `GET /service-categories/:id/children`), путь пройденных категорий
  отображается «хлебными крошками» с возможностью вернуться на любой уровень;
- **услуга** (`VARIANT`) не разворачивается, а выбирается: сразу показывается её
  цена, и по ней можно оформить заказ. Для аукционных услуг вместо цены —
  пометка «Аукцион», выбор заблокирован (отдельный поток заказа по фото в
  приложении пока не реализован);
- срочность (`is_urgent`) предлагается только после выбора неаукционной услуги.

Состояние навигации в дашборде — это `catalogItems` (узлы текущего уровня) и
`catalogPath` (стек открытых категорий); ветки `openCatalogNode` /
`goToCatalogLevel` загружают уровни и держат крошки в актуальном состоянии.

### 6.3 Создание заказа

```typescript
const submitOrder = async () => {
  if (!selectedVariantId.value) {
    errorMsg.value = t('customer.errorInvalidOrderType')
    return
  }

  const payload: any = {
    service_variant_id: selectedVariantId.value,
    is_urgent: !isAuctionSelected.value && isUrgent.value,
    is_asap: !isAuctionSelected.value && isAsap.value,
    address: orderAddress.value,
  }

  if (orderLat.value !== null && orderLon.value !== null) {
    payload.lat = orderLat.value
    payload.lon = orderLon.value
  }

  await api.post('/customer/orders', payload)
  // ...
}
```

> **Важно:** клиент не должен отправлять `is_urgent`/`is_asap` для аукционных вариантов. Бэкенд всё равно должен игнорировать эти флаги для `is_auction = TRUE`.

### 6.4 Отображение заказов

Вместо `getTypeKey(volume_type, speed_tariff)`:

```typescript
const formatOrderType = (order: any) => {
  const variant = order.service_variant
  if (!variant) return order.service_variant_id
  const name = localizedName(variant)
  if (order.is_asap) return `${name} (${t('customer.asap')})`
  if (order.is_urgent) return `${name} (${t('customer.urgent')})`
  return name
}
```

Для полного пути «Категория → Вид» бэкенд может возвращать `service_variant_path` в ответе по заказу.

В `ExecutorDashboard.vue` заменить отображение сырых `volume_type`/`speed_tariff` на:

```vue
<div class="text-xs text-secondary">
  {{ formatOrderType(order) }}
</div>
```

### 6.5 Обновление админских таблиц

Файлы `frontend/src/pages/admin/ActiveOrders.vue`, `CompletedOrders.vue`:

Заменить колонки:

```typescript
{ key: 'volume_type', label: t('admin.volumeType') },
{ key: 'speed_tariff', label: t('admin.speedTariff') },
```

на:

```typescript
{ key: 'service_variant_name', label: t('admin.serviceType') },
{ key: 'is_urgent', label: t('admin.urgent') },
{ key: 'is_asap', label: t('admin.asap') },
```

Бэкенд должен возвращать `service_variant_name` (или `service_variant`) в ответах `/admin/orders/active` и `/admin/orders/completed`.

### 6.6 Новый раздел админки: Каталог услуг

Создать файлы:

- `frontend/src/pages/admin/ServiceCatalog.vue` — дерево каталога
- `frontend/src/pages/admin/ServiceNodeForm.vue` — форма создания/редактирования
- `frontend/src/api/admin-services.ts` — CRUD API

Функциональность:

- Древовидное отображение категорий и видов.
- Кнопки «Добавить категорию», «Добавить вид».
- Редактирование названий на всех языках, описания, цены, сортировки, активности.
- Удаление с проверкой на дочерние узлы и связанные заказы.

Форма узла:

| Поле | Тип | Доступно для |
|------|-----|--------------|
| Родитель | select | CATEGORY, VARIANT |
| Code | text (только чтение при редактировании) | все |
| Название (ru) | text | все |
| Название (en) | text | все |
| Описание (ru) | textarea | все |
| Описание (en) | textarea | все |
| Тип узла | select (только при создании) | все |
| Базовая цена | number | VARIANT |
| Аукцион | checkbox | VARIANT |
| Активен | checkbox | все |
| Порядок | number | все |

### 6.7 Удаление устаревшей локализации

Удалить из `frontend/src/i18n/locales/ru.json` и `en.json`:

```json
"types": {
  "standardRegular": "...",
  "standardUrgent": "...",
  ...
},
"volumeStandard": "...",
"volumeLarge": "...",
"volumeConstruction": "...",
"tariffRegular": "...",
"tariffUrgent": "...",
"tariffAsap": "..."
```

Добавить новые ключи:

```json
"serviceType": "Тип услуги",
"urgent": "Срочный",
"asap": "ASAP",
"serviceCatalog": "Каталог услуг"
```

### 6.8 Android (Capacitor)

Приложение `frontend/android/` — это Capacitor-обёртка над Vue 3 SPA. Скомпилированные web-ресурсы размещаются в `frontend/android/app/src/main/assets/public/`. Поэтому все изменения UI, API-клиента и локализации выполняются в общем фронтенд-коде (`frontend/src/`) и автоматически попадают в Android-приложение при сборке APK.

Что нужно проверить/обновить в Android-контексте:

1. **Сборка APK:** после изменений `frontend/src/` выполнить:
   ```bash
   cd frontend
   npm run build
   npx cap sync android
   cd android
   ./gradlew assembleRelease
   ```
2. **Кэш WebView:** при OTA-обновлении приложение может показывать старую версию. Рассмотреть принудительный `reload()` или версионирование статики.
3. **Нативные разрешения:** аукционные и срочные заказы могут требовать push-уведомлений. Убедиться, that `AndroidManifest.xml` содержит разрешения для геолокации и уведомлений.
4. **Проверка на устройстве:** протестировать создание заказа, выбор категории/вида, отображение списка заказов и аукционы на Android.

### 6.9 Автообновление Android-приложения

#### CI/CD: автоматическая регистрация при сборке

Для того чтобы при сборке APK автоматически появлялась запись в БД, добавлена CLI-утилита `backend/cmd/release/main.go` и target `make release-android`.

```bash
make release-android VERSION=1.0.1
```

Что делает эта команда:
1. Вызывает `make build-android` (сборка web + Android APK).
2. Берёт основную версию из переменной `VERSION` (например, `1.0.1`).
3. Автоматически вычисляет `versionCode` как `MAX(version_code) + 1` для платформы.
4. Копирует APK в `releases/android/app-release-<version>-<code>.apk` (в корне проекта).
5. Подключается к PostgreSQL (через переменные окружения `DB_*`) и создаёт запись в `mobile_app_releases`.
6. Деактивирует предыдущий активный релиз.

> **Важно:** перед первым запуском `make release-android` должна быть применена миграция `backend/migrations/012_service_catalog.sql` (команда `make migrate`).
>
> **Важно:** `make build-android` **не** регистрирует релиз в БД. Она только собирает APK. Для регистрации используйте `make release-android VERSION=X.Y.Z`.
>
> **Важно:** `versionName` теперь передаётся явно через `VERSION`. `versionCode` генерируется автоматически, поэтому повторные сборки одной версии не вызовут конфликт уникальности.

#### Архитектура

1. Backend хранит актуальный APK в папке `releases/android/` (в корне проекта) и метаданные релиза в таблице `mobile_app_releases`.
2. Приложение раз в минуту запрашивает `GET /app/version?platform=android`.
3. Если `version_code` на сервере больше текущего `versionCode` приложения, показываем диалог с предложением обновиться.
4. Пользователь соглашается → скачиваем APK → запускаем системный установщик через `Intent.ACTION_INSTALL_PACKAGE` + `FileProvider`.

#### Поля `force_update` и `release_notes`

| Поле | Тип | Назначение | Использование |
|------|-----|-----------|---------------|
| `force_update` | BOOLEAN | Принудительное обновление | Если `true`, приложение показывает модальный диалог без кнопки «Позже/Отмена». Пользователь не может пользоваться приложением, пока не установит обновление. Используется при критических багах, breaking changes в API или исправлениях безопасности. |
| `release_notes` | TEXT | Описание изменений | Показывается в диалоге обновления, чтобы пользователь понимал, зачем обновляться. Может содержать plain text или markdown. |

Примеры использования при сборке:

```bash
# Обычное обновление с описанием изменений
make release-android VERSION=1.0.2 RELEASE_NOTES="Исправлены мелкие баги и улучшена производительность"

# Принудительное обновление
make release-android VERSION=1.0.2 FORCE_UPDATE=1 RELEASE_NOTES="Критическое исправление безопасности. Обновление обязательно."
```

В UI:
- `force_update = false` — показывается dismissible banner с кнопкой «Установить».
- `force_update = true` — показывается модальный диалог на весь экран, который нельзя закрыть. Только кнопка «Установить».
- `release_notes` отображается под заголовком диалога/banner.

#### Backend: раздача APK

Файл APK должен быть доступен по прямой ссылке. Варианты:

- **Nginx** (рекомендуется): добавить в `nginx.conf`:
  ```nginx
  location /releases/ {
      alias /app/releases/;
      add_header Content-Type application/vnd.android.package-archive;
  }
  ```
- **Go backend:** `GET /releases/android/:file` через `http.ServeFile`.

#### Нативная часть

Создать custom Capacitor plugin или нативный bridge: `frontend/android/app/src/main/java/com/healthlogin/app/UpdatePlugin.java`.

**AndroidManifest.xml** — добавить разрешения:

```xml
<uses-permission android:name="android.permission.REQUEST_INSTALL_PACKAGES" />
<uses-permission android:name="android.permission.DOWNLOAD_WITHOUT_NOTIFICATION" />

<application ...>
    <!-- FileProvider уже есть, расширить file_paths.xml -->
    <provider
        android:name="androidx.core.content.FileProvider"
        android:authorities="${applicationId}.fileprovider"
        android:exported="false"
        android:grantUriPermissions="true">
        <meta-data
            android:name="android.support.FILE_PROVIDER_PATHS"
            android:resource="@xml/file_paths" />
    </provider>
</application>
```

**res/xml/file_paths.xml:**

```xml
<?xml version="1.0" encoding="utf-8"?>
<paths xmlns:android="http://schemas.android.com/apk/res/android">
    <external-path name="my_images" path="." />
    <cache-path name="my_cache_images" path="." />
    <files-path name="updates" path="updates/" />
</paths>
```

**UpdatePlugin.java** (упрощённый пример):

```java
package com.healthlogin.app;

import android.app.Activity;
import android.app.DownloadManager;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.os.Environment;
import androidx.core.content.FileProvider;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;
import java.io.File;

@CapacitorPlugin(name = "AppUpdate")
public class UpdatePlugin extends Plugin {

    @PluginMethod
    public void getCurrentVersion(PluginCall call) {
        try {
            PackageInfo pInfo = getContext().getPackageManager().getPackageInfo(
                    getContext().getPackageName(), 0);
            JSObject ret = new JSObject();
            ret.put("versionName", pInfo.versionName);
            ret.put("versionCode", Build.VERSION.SDK_INT >= Build.VERSION_CODES.P
                    ? pInfo.getLongVersionCode() : pInfo.versionCode);
            call.resolve(ret);
        } catch (PackageManager.NameNotFoundException e) {
            call.reject(e.getMessage());
        }
    }

    @PluginMethod
    public void downloadAndInstall(PluginCall call) {
        String url = call.getString("url");
        if (url == null) {
            call.reject("url is required");
            return;
        }

        DownloadManager.Request request = new DownloadManager.Request(Uri.parse(url));
        request.setMimeType("application/vnd.android.package-archive");
        request.setTitle("Updating HealthLogin");
        request.setDestinationInExternalFilesDir(getContext(), "updates", "update.apk");
        request.setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE);

        DownloadManager dm = (DownloadManager) getContext().getSystemService(Context.DOWNLOAD_SERVICE);
        long downloadId = dm.enqueue(request);

        BroadcastReceiver receiver = new BroadcastReceiver() {
            @Override
            public void onReceive(Context context, Intent intent) {
                long id = intent.getLongExtra(DownloadManager.EXTRA_DOWNLOAD_ID, -1);
                if (id != downloadId) return;

                File apk = new File(getContext().getExternalFilesDir("updates"), "update.apk");
                Uri apkUri = FileProvider.getUriForFile(
                        getContext(),
                        getContext().getPackageName() + ".fileprovider",
                        apk);

                Intent install = new Intent(Intent.ACTION_INSTALL_PACKAGE);
                install.setData(apkUri);
                install.setFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_ACTIVITY_NEW_TASK);
                getContext().startActivity(install);

                getContext().unregisterReceiver(this);
                call.resolve();
            }
        };

        getContext().registerReceiver(receiver, new IntentFilter(DownloadManager.ACTION_DOWNLOAD_COMPLETE));
    }
}
```

> **Важно:** на Android 8.0+ необходимо получить разрешение `REQUEST_INSTALL_PACKAGES` через системный диалог. Если разрешение не granted, перенаправить пользователя в настройки:
> ```java
> Intent intent = new Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES)
>         .setData(Uri.parse("package:" + getContext().getPackageName()));
> getContext().startActivity(intent);
> ```

#### TypeScript bridge

Создать `frontend/src/plugins/app-update.ts`:

```typescript
import { registerPlugin } from '@capacitor/core'

export interface AppUpdatePlugin {
  getCurrentVersion(): Promise<{ versionName: string; versionCode: number }>
  downloadAndInstall(options: { url: string }): Promise<void>
}

export const AppUpdate = registerPlugin<AppUpdatePlugin>('AppUpdate')
```

#### Периодическая проверка версии

Создать `frontend/src/composables/useAppUpdate.ts`:

```typescript
import { ref, onMounted, onUnmounted } from 'vue'
import { AppUpdate } from '@/plugins/app-update'
import { api } from '@/services/api'
import { useI18n } from 'vue-i18n'

const CHECK_INTERVAL_MS = 60_000

export function useAppUpdate() {
  const { t } = useI18n()
  const updateInfo = ref<any>(null)
  let intervalId: number | undefined

  const checkVersion = async () => {
    try {
      const current = await AppUpdate.getCurrentVersion()
      const response = await api.get('/app/version?platform=android')
      const latest = response.data

      if (latest.version_code > current.versionCode) {
        updateInfo.value = latest
      }
    } catch (e) {
      console.error('Version check failed', e)
    }
  }

  onMounted(() => {
    checkVersion()
    intervalId = window.setInterval(checkVersion, CHECK_INTERVAL_MS)
  })

  onUnmounted(() => {
    if (intervalId) clearInterval(intervalId)
  })

  const installUpdate = () => {
    if (!updateInfo.value) return
    AppUpdate.downloadAndInstall({ url: updateInfo.value.download_url })
  }

  return { updateInfo, installUpdate }
}
```

#### UI-диалог и версия внизу экрана

Компонент `UpdateBanner` добавляется глобально в `App.vue`, чтобы проверка обновлений работала на всех экранах (login, dashboard и т.д.), а не только внутри дашбордов.

`App.vue`:

```vue
<template>
  <ServerStatusIndicator />
  <UpdateBanner />
  <router-view />
  <AppVersionFooter />
</template>
```

Компонент `AppVersionFooter` отображает текущую версию приложения внизу экрана маленьким шрифтом:

```vue
<div class="app-version-footer">
  <span>v{{ version }}</span>
  <span>(build {{ build }})</span>
</div>
```

#### Кнопка обновления рядом с индикатором сервера

`ServerStatusIndicator.vue` теперь также проверяет `/app/version` и показывает оранжевую пульсирующую кнопку с иконкой ⬆, если на сервере есть более новая версия. При нажатии запускается установка обновления.

#### Явная регистрация плагина в Android

Если auto-discovery Capacitor не подхватывает `UpdatePlugin`, зарегистрируйте его явно в `MainActivity.java`:

```java
package com.healthlogin.app;

import android.os.Bundle;
import com.getcapacitor.BridgeActivity;
import com.healthlogin.app.plugins.UpdatePlugin;

public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        registerPlugin(UpdatePlugin.class);
        super.onCreate(savedInstanceState);
    }
}
```

#### Runtime-разрешение `REQUEST_INSTALL_PACKAGES`

На Android 8.0+ (API 26) разрешения `<uses-permission android:name="android.permission.REQUEST_INSTALL_PACKAGES" />` недостаточно. Нужно проверять `canRequestPackageInstalls()` и, если разрешение не выдано, открывать настройки приложения через `Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES`.

`UpdatePlugin.downloadAndInstall` выполняет эту проверку перед запуском скачивания.

#### Формирование URL для скачивания

Backend может возвращать либо абсолютный `download_url`, либо относительный путь. Для гибкости фронтенд дополняет относительный путь базовым `VITE_API_URL`:

```typescript
function resolveDownloadUrl(url: string): string {
  if (url.startsWith('http://') || url.startsWith('https://')) return url
  const base = import.meta.env.VITE_API_URL || ''
  return `${base}${base.endsWith('/') || url.startsWith('/') ? '' : '/'}${url}`
}
```

> **Рекомендация:** в `docker-compose.yml` оставить `RELEASES_BASE_URL` пустым (по умолчанию), чтобы backend отдавал относительный путь, а frontend формировал URL под конкретное окружение (web, эмулятор, реальное устройство).

#### Локализация

Добавить ключи:

```json
"app": {
  "updateAvailable": "Доступно обновление {version}",
  "installUpdate": "Установить",
  "updateRequired": "Требуется обновление приложения"
}
```

---

## 7. Миграции

### 7.1 Новый файл миграции

Создать `backend/migrations/012_service_catalog.sql`:

```sql
-- 012_service_catalog.sql
-- Service catalog with hierarchical structure

CREATE TABLE IF NOT EXISTS service_nodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_id UUID REFERENCES service_nodes(id),
    code VARCHAR UNIQUE NOT NULL,
    name JSONB NOT NULL,
    description JSONB,
    node_type VARCHAR NOT NULL CHECK (node_type IN ('CATEGORY', 'VARIANT')),
    base_price NUMERIC(18,2),
    is_auction BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT chk_variant_has_price
        CHECK (node_type != 'VARIANT' OR base_price IS NOT NULL),
    CONSTRAINT chk_category_no_price
        CHECK (node_type != 'CATEGORY' OR base_price IS NULL)
);

CREATE TABLE IF NOT EXISTS service_node_paths (
    ancestor_id UUID REFERENCES service_nodes(id) ON DELETE CASCADE,
    descendant_id UUID REFERENCES service_nodes(id) ON DELETE CASCADE,
    depth INT NOT NULL,
    PRIMARY KEY (ancestor_id, descendant_id)
);

CREATE INDEX idx_service_node_paths_ancestor ON service_node_paths(ancestor_id);
CREATE INDEX idx_service_node_paths_descendant ON service_node_paths(descendant_id);

CREATE TABLE IF NOT EXISTS mobile_app_releases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    platform VARCHAR NOT NULL CHECK (platform IN ('android')),
    version_name VARCHAR NOT NULL,
    version_code INT NOT NULL,
    file_name VARCHAR NOT NULL,
    file_path VARCHAR NOT NULL,
    release_notes TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    force_update BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_mobile_app_releases_active
    ON mobile_app_releases(platform, version_name)
    WHERE is_active = TRUE;

CREATE OR REPLACE FUNCTION rebuild_service_node_paths(p_node_id UUID)
RETURNS VOID AS $$
BEGIN
    -- 1. Удаляем все старые пути для узла и его поддерева.
    DELETE FROM service_node_paths
    WHERE descendant_id IN (
        SELECT id FROM service_nodes WHERE id = p_node_id
        UNION ALL
        WITH RECURSIVE descendants AS (
            SELECT id FROM service_nodes WHERE parent_id = p_node_id
            UNION ALL
            SELECT sn.id FROM service_nodes sn
            JOIN descendants d ON sn.parent_id = d.id
        )
        SELECT id FROM descendants
    );

    -- 2. Путь узла к самому себе (depth 0).
    INSERT INTO service_node_paths (ancestor_id, descendant_id, depth)
    VALUES (p_node_id, p_node_id, 0);

    -- 3. Пути от всех предков узла к самому узлу.
    INSERT INTO service_node_paths (ancestor_id, descendant_id, depth)
    SELECT sp.ancestor_id, p_node_id, sp.depth + 1
    FROM service_node_paths sp
    WHERE sp.descendant_id = (SELECT parent_id FROM service_nodes WHERE id = p_node_id);

    -- 4. Пути от самого узла ко всем его потомкам.
    WITH RECURSIVE descendants AS (
        SELECT id, parent_id, 1 AS depth
        FROM service_nodes
        WHERE parent_id = p_node_id
        UNION ALL
        SELECT sn.id, sn.parent_id, d.depth + 1
        FROM service_nodes sn
        JOIN descendants d ON sn.parent_id = d.id
    )
    INSERT INTO service_node_paths (ancestor_id, descendant_id, depth)
    SELECT p_node_id, d.id, d.depth
    FROM descendants d;

    -- 5. Пути от предков узла (кроме него самого) ко всем его потомкам.
    WITH RECURSIVE descendants AS (
        SELECT id, parent_id, 1 AS depth
        FROM service_nodes
        WHERE parent_id = p_node_id
        UNION ALL
        SELECT sn.id, sn.parent_id, d.depth + 1
        FROM service_nodes sn
        JOIN descendants d ON sn.parent_id = d.id
    )
    INSERT INTO service_node_paths (ancestor_id, descendant_id, depth)
    SELECT a.ancestor_id, d.id, a.depth + d.depth
    FROM descendants d
    JOIN (
        SELECT ancestor_id, depth
        FROM service_node_paths
        WHERE descendant_id = p_node_id AND ancestor_id <> p_node_id
    ) a ON TRUE;
END;
$$ LANGUAGE plpgsql;

-- Seed data
INSERT INTO service_nodes (id, parent_id, code, name, description, node_type, base_price, is_auction, is_active, sort_order) VALUES
('11111111-1111-1111-1111-111111111111', NULL, 'trash_removal',
 '{"ru": "Вынос мусора", "en": "Trash removal"}',
 '{"ru": "Вывоз бытовых отходов", "en": "Household waste removal"}',
 'CATEGORY', NULL, FALSE, TRUE, 1),
('22222222-2222-2222-2222-222222222222', '11111111-1111-1111-1111-111111111111', 'trash_standard',
 '{"ru": "Стандартный", "en": "Standard"}',
 '{"ru": "Стандартный объём", "en": "Standard volume"}',
 'CATEGORY', NULL, FALSE, TRUE, 1),
('33333333-3333-3333-3333-333333333333', '22222222-2222-2222-2222-222222222222', 'trash_standard_single',
 '{"ru": "Один пакет", "en": "Single bag"}',
 '{"ru": "До 30 литров", "en": "Up to 30 liters"}',
 'VARIANT', 100.00, FALSE, TRUE, 1),
('44444444-4444-4444-4444-444444444444', '22222222-2222-2222-2222-222222222222', 'trash_standard_double',
 '{"ru": "Двойной пакет", "en": "Double bag"}',
 '{"ru": "До 60 литров", "en": "Up to 60 liters"}',
 'VARIANT', 180.00, FALSE, TRUE, 2),
('55555555-5555-5555-5555-555555555555', '11111111-1111-1111-1111-111111111111', 'trash_large',
 '{"ru": "Большой", "en": "Large"}',
 '{"ru": "Крупногабаритный мусор", "en": "Bulky waste"}',
 'CATEGORY', NULL, FALSE, TRUE, 2),
('66666666-6666-6666-6666-666666666666', '55555555-5555-5555-5555-555555555555', 'trash_large_regular',
 '{"ru": "Большой обычный", "en": "Large regular"}',
 NULL, 'VARIANT', 200.00, FALSE, TRUE, 1),
('77777777-7777-7777-7777-777777777777', '11111111-1111-1111-1111-111111111111', 'trash_construction',
 '{"ru": "Строительный мусор", "en": "Construction waste"}',
 '{"ru": "Аукцион исполнителей", "en": "Executor auction"}',
 'VARIANT', 0.00, TRUE, TRUE, 3),
('88888888-8888-8888-8888-888888888888', NULL, 'dog_walking',
 '{"ru": "Выгул собак", "en": "Dog walking"}',
 NULL, 'CATEGORY', NULL, FALSE, TRUE, 2),
('99999999-9999-9999-9999-999999999999', '88888888-8888-8888-8888-888888888888', 'dog_walk_morning',
 '{"ru": "Утренний выгул", "en": "Morning walk"}',
 NULL, 'VARIANT', 150.00, FALSE, TRUE, 1),
('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', '88888888-8888-8888-8888-888888888888', 'dog_walk_evening',
 '{"ru": "Вечерний выгул", "en": "Evening walk"}',
 NULL, 'VARIANT', 150.00, FALSE, TRUE, 2),
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', '88888888-8888-8888-8888-888888888888', 'dog_overnight',
 '{"ru": "Передержка", "en": "Overnight stay"}',
 NULL, 'VARIANT', 800.00, FALSE, TRUE, 3);

-- Build closure table
SELECT rebuild_service_node_paths('11111111-1111-1111-1111-111111111111');
SELECT rebuild_service_node_paths('22222222-2222-2222-2222-222222222222');
SELECT rebuild_service_node_paths('33333333-3333-3333-3333-333333333333');
SELECT rebuild_service_node_paths('44444444-4444-4444-4444-444444444444');
SELECT rebuild_service_node_paths('55555555-5555-5555-5555-555555555555');
SELECT rebuild_service_node_paths('66666666-6666-6666-6666-666666666666');
SELECT rebuild_service_node_paths('77777777-7777-7777-7777-777777777777');
SELECT rebuild_service_node_paths('88888888-8888-8888-8888-888888888888');
SELECT rebuild_service_node_paths('99999999-9999-9999-9999-999999999999');
SELECT rebuild_service_node_paths('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa');
SELECT rebuild_service_node_paths('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb');

-- Update orders table
ALTER TABLE orders
    ADD COLUMN service_variant_id UUID REFERENCES service_nodes(id),
    ADD COLUMN is_urgent BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN is_asap BOOLEAN NOT NULL DEFAULT FALSE;

-- Миграция данных. Для больших таблиц заменить на батчевый UPDATE ниже.
UPDATE orders
SET
    service_variant_id = CASE
        WHEN volume_type = 'STANDARD' THEN '33333333-3333-3333-3333-333333333333'::UUID
        WHEN volume_type = 'LARGE' THEN '66666666-6666-6666-6666-666666666666'::UUID
        WHEN volume_type = 'CONSTRUCTION' THEN '77777777-7777-7777-7777-777777777777'::UUID
    END,
    is_urgent = CASE WHEN speed_tariff = 'URGENT' THEN TRUE ELSE FALSE END,
    is_asap = CASE WHEN speed_tariff = 'ASAP' THEN TRUE ELSE FALSE END;

-- Батчевый вариант для больших таблиц (выполнять вместо одного UPDATE выше):
-- DO $$
-- DECLARE
--     rows_updated INT;
-- BEGIN
--     LOOP
--         WITH batch AS (
--             SELECT id FROM orders
--             WHERE service_variant_id IS NULL
--             LIMIT 5000
--         )
--         UPDATE orders o
--         SET
--             service_variant_id = CASE
--                 WHEN volume_type = 'STANDARD' THEN '33333333-3333-3333-3333-333333333333'::UUID
--                 WHEN volume_type = 'LARGE' THEN '66666666-6666-6666-6666-666666666666'::UUID
--                 WHEN volume_type = 'CONSTRUCTION' THEN '77777777-7777-7777-7777-777777777777'::UUID
--             END,
--             is_urgent = CASE WHEN speed_tariff = 'URGENT' THEN TRUE ELSE FALSE END,
--             is_asap = CASE WHEN speed_tariff = 'ASAP' THEN TRUE ELSE FALSE END
--         FROM batch b
--         WHERE o.id = b.id;
--
--         GET DIAGNOSTICS rows_updated = ROW_COUNT;
--         EXIT WHEN rows_updated = 0;
--     END LOOP;
-- END $$;

-- Проверяем, что не осталось строк без маппинга
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM orders WHERE service_variant_id IS NULL) THEN
        RAISE EXCEPTION 'Found orders without service_variant_id mapping';
    END IF;
END $$;

ALTER TABLE orders
    ALTER COLUMN service_variant_id SET NOT NULL;

ALTER TABLE orders
    DROP COLUMN volume_type,
    DROP COLUMN speed_tariff;

DROP TYPE IF EXISTS volume_type;

-- Проверяем, что speed_tariff_type больше не используется, и удаляем
-- (раскомментировать после проверки зависимостей):
-- DROP TYPE IF EXISTS speed_tariff_type;

-- Seed tariff coefficients if missing
INSERT INTO system_settings (key, value)
VALUES
    ('urgent_tariff_coeff', '3.0'),
    ('asap_tariff_coeff', '8.0')
ON CONFLICT (key) DO NOTHING;
```

### 7.2 Порядок применения

1. Сделать резервную копию БД.
2. В окно обслуживания остановить приложение (бэкенд + фронтенд).
3. Выполнить `backend/migrations/012_service_catalog.sql`.
4. Задеплоить новую версию бэкенда.
5. Задеплоить новую версию фронтенда (web + `cap sync android` + APK).
6. Проверить работу каталога, создание заказа, аукционы, админку.
7. Подготовить план отката: восстановление БД из бэкапа + деплой предыдущей версии кода.

> **Zero-downtime:** текущий план предполагает остановку приложения, потому что старая версия кода не сможет работать с новой схемой `orders` после миграции. Если требуется отказоустойчивое развёртывание, рассмотреть двухэтапную миграцию:
> 1. Добавить новые колонки, оставить старые, развернуть код, который пишет в обе схемы.
> 2. Удалить старые колонки после стабилизации.

---

## 8. Тестирование

### 8.1 Модульные тесты бэкенда

Файл `backend/service/order_test.go`:

- Создание заказа с `is_urgent = false`, `is_asap = false` — цена равна `base_price`.
- Создание заказа с `is_urgent = true` — цена равна `base_price * urgent_tariff_coeff`.
- Создание заказа с `is_asap = true` — цена равна `base_price * asap_tariff_coeff`.
- Даунгрейд ASAP-заказа — цена возвращается к `base_price`.
- Создание аукционного заказа — `hold_amount = 0`.
- Невозможность создать заказ с неактивным видом.
- Невозможность создать заказ с `is_urgent = true` и `is_asap = true` одновременно.

Файл `backend/repository/service_catalog_test.go` (новый):

- `CreateNode` создаёт узел и корректные замыкания.
- `UpdateNode` при смене `parent_id` корректно пересчитывает пути для всего поддерева.
- Попытка создать цикл (`parent_id` = потомок) возвращает ошибку.
- Удаление категории с дочерними узлами запрещено.
- Удаление вида с заказами запрещено FK.

### 8.2 Интеграционные тесты

- Загрузка каталога на фронтенде.
- Выбор категории → вида → флага срочности/ASAP.
- Создание заказа через API (обычный, срочный, ASAP, аукцион).
- Отображение заказа в админке.
- Создание/редактирование/удаление узлов в админке.
- Сборка Android APK и smoke-тест на устройстве/эмуляторе.
- Загрузка APK через `/admin/app-releases` и корректность `GET /app/version`.
- Сценарий автообновления: диалог → скачивание → установка.

### 8.3 Регрессионные проверки

- Заказчик: создание, отмена, подтверждение заказа.
- Исполнитель: просмотр доступных заказов, ставки на аукцион, назначение.
- Админ: финансы, транзакции, чаты.
- SLA Worker: даунгрейд ускоренного заказа.

---

## 9. Риски и ограничения

| Риск | Влияние | Митигация |
|------|---------|-----------|
| Долгий UPDATE orders на большой таблице | Простой | Выполнять батчами или в окно обслуживания |
| Ошибка в `rebuild_service_node_paths` | Некорректное дерево | Покрыть тестами, выполнять в транзакции, проверять циклы |
| Несоответствие цен после миграции | Финансовые потери | Сверить seed-цены с текущими `basePriceByVolume` и коэффициентами |
| Потеря различия URGENT/ASAP | Некорректная цена | Использовать оба флага `is_urgent` и `is_asap` |
| Пользователь не обновит фронтенд / Android APK | Ошибки UI | Версионирование API, принудительный reload, обновление APK |
| Удаление используемого вида | Нарушение FK | Блокировать удаление в админке и БД |
| Циклы в дереве каталога | Бесконечная рекурсия | Валидация parent_id в приложении и тесты |
| Несовместимость старого APK с новым API | Ошибки в мобильном приложении | Требовать обновления APK или поддерживать обратную совместимость до полного перехода |
| Ошибка в механизме автообновления | Невозможность установить обновление | Тестировать на Android 8+, 10+, 12+; запрашивать `REQUEST_INSTALL_PACKAGES` |
| Подпись APK изменена | Android откажется устанавливать обновление | Использовать один и тот же signing key для всех релизов |
| Недоступность `/releases/` по HTTPS | Скачивание не работает | Настроить nginx и SSL-сертификат; проверить CORS/брандмауэр |

---

## 10. Assumptions & Constraints

- **H1**: Closure Table — выбранная схема для иерархии произвольной глубины.
- **H2**: Поля `code` и `node_type` не редактируются после создания узла.
- **H3**: Динамические модификаторы цены — два уровня срочности (`is_urgent`, `is_asap`) плюс даунгрейд ASAP; коэффициенты хранятся в `system_settings`.
- **H4**: Локализация хранится в JSONB-полях `name` и `description`; поддерживаются `ru` и `en`.
- **H5**: Удаление вида запрещено при наличии связанных заказов.
- **H6**: Аукционный вариант имеет `base_price = 0` и не использует флаги срочности.
- **H7**: Android-приложение — Capacitor-обёртка над web-фронтендом; изменения в `frontend/src/` покрывают и мобильное приложение.
- **H8**: Seed-данные фиксированы; `trash_standard_double` — новая опция, не маппится из старых данных.
- **H9**: APK-приложение подписано одним и тем же ключом для всех релизов; иначе автообновление невозможно.
- **H10**: Пользователь может устанавливать приложения из неизвестных источников (разрешение `REQUEST_INSTALL_PACKAGES`).
- **Constraint C1**: Таблица `orders` может быть большой — миграция требует планирования и батчинга.
- **Constraint C2**: Админка для каталога реализуется в рамках существующей Vue-админки.
- **Constraint C3**: Zero-downtime развёртывание не предусмотрено; требуется окно обслуживания.
- **Constraint C4**: Автообновление Android работает только при наличии сети и доступности `/releases/` по HTTPS.

---

## 11. Итоговый чек-лист

### Бэкенд
- [ ] Создать `backend/migrations/012_service_catalog.sql` (с учётом `is_asap`, батчинга и `mobile_app_releases`).
- [ ] Создать `backend/migrations/013_update_app_release_indexes.sql` (уникальность по `version_code`).
- [ ] Создать `backend/repository/service_catalog.go` с интерфейсом и транзакционным `CreateNode`.
- [ ] Создать `backend/repository/app_release.go` и `backend/handler/app_release.go`.
- [ ] Реализовать проверку циклов и корректный `rebuild_service_node_paths`.
- [ ] Обновить `backend/repository/order.go`, `admin.go`, `bid.go` на `service_variant_id`, `is_urgent`, `is_asap`.
- [ ] Обновить `backend/service/order.go` (ценообразование, ASAP-даунгрейд, создание заказа).
- [ ] Обновить `backend/service/bid.go` (аукционы по `is_auction`).
- [ ] Добавить публичные API-эндпоинты каталога.
- [ ] Добавить админские API-эндпоинты каталога с валидацией.
- [ ] Добавить endpoint `/app/version` и `/admin/app-releases`.
- [ ] Добавить тесты `backend/repository/service_catalog_test.go`.
- [ ] Обновить `backend/service/order_test.go`.

### Фронтенд и Android
- [ ] Создать `frontend/src/api/services.ts`.
- [ ] Обновить `frontend/src/pages/customer/CustomerDashboard.vue` (динамический каталог, `is_asap`).
- [ ] Обновить `frontend/src/pages/executor/ExecutorDashboard.vue` (отображение вида, аукционы).
- [ ] Обновить `frontend/src/pages/admin/ActiveOrders.vue` и `CompletedOrders.vue`.
- [ ] Создать `frontend/src/pages/admin/ServiceCatalog.vue`.
- [ ] Создать `frontend/src/pages/admin/ServiceNodeForm.vue`.
- [ ] Обновить локализацию `frontend/src/i18n/locales/*.json`.
- [ ] Создать `frontend/src/plugins/app-update.ts` и `frontend/src/composables/useAppUpdate.ts`.
- [ ] Создать `frontend/src/components/AppVersionFooter.vue`.
- [ ] Добавить `UpdatePlugin.java` в `frontend/android/app/src/main/java/com/healthlogin/app/`.
- [ ] Обновить `AndroidManifest.xml` (`REQUEST_INSTALL_PACKAGES`, FileProvider).
- [ ] Обновить `res/xml/file_paths.xml` (`<files-path name="updates" path="updates/" />`).
- [ ] Добавить `UpdateBanner` и `AppVersionFooter` в `App.vue`.
- [ ] Убрать дублирующий `UpdateBanner` из `CustomerDashboard.vue` и `ExecutorDashboard.vue`.
- [ ] Обновить `ServerStatusIndicator.vue` (кнопка обновления).
- [ ] Обновить `MainActivity.java` (явная регистрация `UpdatePlugin`).
- [ ] Выполнить `npm run build && npx cap sync android` и собрать APK.
- [ ] Выполнить `make release-android` и убедиться, что запись появилась в `mobile_app_releases`.
- [ ] Провести smoke-тест на Android-устройстве/эмуляторе.
- [ ] Протестировать сценарий автообновления (загрузка APK, сравнение версий, установка).

### Инфраструктура и тестирование
- [ ] Обновить `nginx.conf` (проксирование `/app`, раздача `/releases/`).
- [ ] Настроить volume `./releases:/app/releases` в `docker-compose.yml` и `docker-compose.prod.yml`.
- [ ] Сделать резервную копию БД перед миграцией.
- [ ] Провести регрессионное тестирование (заказчик, исполнитель, админ, SLA worker).
- [ ] Подготовить план отката.
- [ ] Задеплоить миграцию и код.
