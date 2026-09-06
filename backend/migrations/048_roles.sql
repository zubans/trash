-- 048_roles.sql
-- Роли становятся строкой справочника, а не значением, зашитым в код.
--
-- До этой миграции роль была зашита дважды. В Go — списком допустимых значений
-- (service.validRoles), а в самой базе — перечислением role_type из миграции
-- 001, в котором всего три значения: CUSTOMER, EXECUTOR, ADMIN. Завести роль
-- «финансист», которому видно только сверку и выводы, можно было лишь
-- пересборкой образа, да и то не до конца: перечисление отвергло бы её.
--
-- Здесь появляется таблица ролей и таблица их прав, а право — это пара «раздел
-- админки + действие» (users.view, gifts.create и т. п.). Каталог разделов
-- остаётся в Go (service/permission.go): он обязан совпадать с тем, что реально
-- охраняют маршруты, поэтому его источник — код, а не строки в базе.
--
-- ADMIN намеренно не получает строк в role_permissions. Он суперпользователь по
-- коду: право, добавленное новой версией, действует для него сразу, и никакой
-- порядок снятия галочек не может запереть администратора снаружи собственной
-- панели.

-- =============================================================================
-- 1. users.role перестаёт быть перечислением
-- =============================================================================

-- Основная роль обязана принимать те же значения, что и набор ролей в
-- user_roles, — иначе роль, заведённая администратором, назначается (user_roles
-- это text), но не может стать основной, и UPDATE падает на перечислении.
-- Пример уже существовал до всякого справочника: MODERATOR появился в миграции
-- 039, но в role_type его нет, поэтому пользователь с одной лишь ролью
-- модератора ронял SetUserRoles.
--
-- Перечисление используется единственным столбцом (проверено по pg_attribute),
-- поэтому преобразование к text ни на что больше не влияет. Сравнение вида
-- `role <> ''` до преобразования привело бы к попытке привести '' к role_type и
-- к ошибке — именно поэтому конверсия стоит первой, до наполнения справочника.
ALTER TABLE users ALTER COLUMN role TYPE TEXT USING role::TEXT;

DROP TYPE IF EXISTS role_type;

-- =============================================================================
-- 2. Справочник ролей и их права
-- =============================================================================

CREATE TABLE IF NOT EXISTS roles (
    code        TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- is_system защищает четыре роли, на которые опираются маршруты и дашборды:
    -- их нельзя удалить.
    is_system   BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_code  TEXT NOT NULL REFERENCES roles(code) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    PRIMARY KEY (role_code, permission)
);

CREATE INDEX IF NOT EXISTS idx_role_permissions_role ON role_permissions(role_code);

-- Четыре роли, которые уже существуют в коде. MODERATOR среди них: в
-- перечислении его не было, но роль живёт в user_roles с миграции 039.
INSERT INTO roles (code, name, description, is_system) VALUES
    ('CUSTOMER',  'Заказчик',       'Создаёт заказы и оплачивает их.',                      true),
    ('EXECUTOR',  'Исполнитель',    'Берёт заказы в работу и получает выплаты.',            true),
    ('MODERATOR', 'Модератор',      'Разбирает проверки личности и спорные подтверждения.', true),
    ('ADMIN',     'Администратор',  'Полный доступ ко всем разделам панели.',               true)
ON CONFLICT (code) DO NOTHING;

-- Преемственность: любое значение роли, которое уже встречается у пользователей,
-- заводится как роль справочника. Без этого внешний ключ ниже отверг бы строку,
-- существующую с первого дня, и миграция упала бы на живой базе.
INSERT INTO roles (code, name, description, is_system)
SELECT DISTINCT role, role, 'Заведена автоматически при переходе на справочник ролей.', false
FROM (
    SELECT role FROM users WHERE role IS NOT NULL AND role <> ''
    UNION
    SELECT role FROM user_roles WHERE role IS NOT NULL AND role <> ''
) existing
ON CONFLICT (code) DO NOTHING;

-- Теперь набор ролей замкнут: назначить можно только то, что есть в справочнике,
-- а удаление роли снимает её со всех, у кого она была (ON DELETE CASCADE в
-- user_roles), не оставляя висящих строк.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'user_roles_role_fkey'
    ) THEN
        ALTER TABLE user_roles
            ADD CONSTRAINT user_roles_role_fkey
            FOREIGN KEY (role) REFERENCES roles(code) ON DELETE CASCADE;
    END IF;
END $$;

-- Основная роль тоже обязана существовать в справочнике. Перечисление role_type
-- давало эту гарантию на уровне базы, и, снимая его, гарантию надо чем-то
-- заменить — иначе ошибка в коде оставит users.role указывающей на роль,
-- которой нет, и такой пользователь молча потеряет свой дашборд. Внешний ключ
-- строже перечисления в одном отношении: он следует справочнику, а не
-- зафиксированному в схеме списку из трёх значений.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'users_role_fkey'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_role_fkey
            FOREIGN KEY (role) REFERENCES roles(code);
    END IF;
END $$;

-- Права модератора: он и сегодня доходит только до разбора проверок, и список
-- ниже ровно это и закрепляет. Остальным несистемным ролям права выдаёт админ.
INSERT INTO role_permissions (role_code, permission) VALUES
    ('MODERATOR', 'escalations.view'),
    ('MODERATOR', 'escalations.edit'),
    ('MODERATOR', 'users.view')
ON CONFLICT DO NOTHING;
