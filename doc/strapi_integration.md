# Интеграция Strapi v5 (Headless CMS)

В рамках ветки `feature/strapi-integration` в систему интегрирован Headless CMS фреймворк **Strapi 5**.

---

## 1. Архитектура и Структура

Приложение Strapi размещено в директории `strapi-backend/`.

### Созданные сущности (Content-Types):

1. **Order (`/api/orders`)**
   - `customer_id` (Integer, Обязательное) — ID заказчика.
   - `executor_id` (Integer) — ID исполнителя.
   - `status` (Enumeration: `SEARCHING`, `ASSIGNED`, `EXECUTED`, `COMPLETED`, `CANCELED`).
   - `category` (Enumeration: `HOUSEHOLD`, `CONSTRUCTION`, `LARGE_ITEMS`, `HAZARDOUS`).
   - `address` (String).
   - `latitude`, `longitude` (Float).
   - `price` (Decimal).
   - `description` (Text).
   - `photo_urls` (JSON).
   - `sla_deadline` (DateTime).

2. **Bid (`/api/bids`)**
   - `order_id` (Integer, Обязательное).
   - `executor_id` (Integer, Обязательное).
   - `amount` (Decimal, Обязательное).
   - `status` (Enumeration: `PENDING`, `ACCEPTED`, `REJECTED`).

3. **Wallet Transaction (`/api/wallet-transactions`)**
   - `user_id` (Integer, Обязательное).
   - `amount` (Decimal, Обязательное).
   - `type` (Enumeration: `DEPOSIT`, `WITHDRAWAL`, `HOLD`, `PAYOUT`, `FINE`).
   - `description` (String).

4. **Geofence (`/api/geofences`)**
   - `name` (String, Обязательное).
   - `type` (Enumeration: `CIRCLE`, `POLYGON`).
   - `coordinates` (JSON).
   - `active` (Boolean).

5. **System Setting (`/api/system-settings`)**
   - `key` (String, Unique).
   - `value` (Text).
   - `description` (String).

---

## 2. Локальный запуск

```bash
cd strapi-backend
npm run develop
```

Панель администратора доступна по адресу: `http://localhost:1337/admin`

---

## 3. Запуск через Docker Compose

Для запуска в составе всей инфраструктуры проекта:

```bash
docker compose up -d --build strapi
```

Проксирование в Nginx настроено на путь `/strapi/` (порт `1337`).

---

## 4. Сборка и Проверка

- Проверка сборки TypeScript и админ-панели:
```bash
cd strapi-backend
npm run build
```
