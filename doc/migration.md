# Database Migrations

## Overview

The project uses PostgreSQL 16. Migrations are applied automatically when the database container starts via `docker-entrypoint-initdb.d`. They can also be applied manually with `psql` or `make migrate`.

## Migration files

| File | Purpose |
| :--- | :--- |
| `001_create_enums.sql` | ENUM types for roles, statuses, order volumes, tariffs, etc. |
| `002_create_tables.sql` | Core tables: `users`, `customer_profiles`, `executor_profiles`, `orders`, `shifts`, `transactions`, `bids`, `chats`, `messages`, `system_settings`. |
| `003_admin_finances.sql` | Top-up requests, transactions, admin user seed, initial settings. |
| `004_geozones_and_orders.sql` | Geozones, order extensions, GPS logs, shift fines. |
| `005_order_pickup_and_executor_radius.sql` | Order address and coordinates, executor search radius. |
| `006_settings_currency.sql` | Convert `system_settings.value` to `VARCHAR`, seed `currency`. |
| `007_customer_address_string.sql` | Convert `customer_profiles.address` to `VARCHAR`. |
| `008_rename_fine_amount.sql` | Rename `fine_amount` setting to `geofence_fine_amount`. |

## How to run manually

```bash
# Local development
make migrate

# Or directly with psql
for f in backend/migrations/*.sql; do
  psql "postgres://healthlogin:healthlogin@localhost:5432/healthlogin" -f "$f"
done
```

## Notes

* All timestamps use `TIMESTAMP WITH TIME ZONE`.
* Monetary values use `NUMERIC(18,2)` to avoid floating-point errors.
* `system_settings.value` is `VARCHAR` and supports both numeric and string settings.
* `users.password` stores a bcrypt hash; plain text must never be persisted.
