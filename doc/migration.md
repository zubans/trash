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
| `009_geocoding_cache.sql` | Cache for geocoding queries (`query`, `address`, `lat`, `lon`). |
| `010_create_chats_for_existing_orders.sql` | Backfill: create a chat room for every order that lacks one. |
| `010_ratings_and_reviews.sql` | Two-sided ratings and reviews. |
| `011_shift_early_exit_penalty.sql` | Fine charged when an executor ends a shift before `planned_end_at`. |
| `012_service_catalog.sql` | Hierarchical service catalog and mobile app releases. |
| `013_update_app_release_indexes.sql` | App release uniqueness moves from `(platform, version_name)` to `(platform, version_code)`. |
| `014_executor_location_interval.sql` | Default interval (seconds) for automatic executor GPS submission. |
| `015_add_message_status.sql` | Chat message status and `read_at` tracking. |
| `016_add_chat_message_attachments.sql` | File attachments on chat messages. |
| `017_soft_delete_messages.sql` | `is_deleted` on `messages` for soft deletion. |
| `018_executor_location_and_geo_alerts.sql` | Executor location history and geo alerts. |
| `019_add_executed_to_order_status.sql` | Add `EXECUTED` to the `order_status_type` ENUM. |
| `020_add_updated_at_to_messages.sql` | `updated_at` on `messages` for edited chat messages. |
| `021_create_withdrawal_requests.sql` | Withdrawal requests and their status ENUM. |
| `022_add_email_verification_and_password_reset.sql` | Email verification, password reset, unique index on non-null emails. |
| `023_pending_email_verification.sql` | `pending_email`: the address is stored separately until the link is followed. |
| `024_security_hardening.sql` | Constraints and columns required by the security hardening pass. |
| `025_consolidate_runtime_ddl.sql` | Consolidates DDL that used to run from repository constructors on every start. |
| `026_normalize_phone_numbers.sql` | Normalise stored phone numbers to a single canonical form. |
| `026_normalize_user_phones.sql` | Normalise legacy phone numbers to `+7XXXXXXXXXX` (moved out of the users repository constructor). |
| `027_refresh_tokens.sql` | Refresh tokens. |
| `028_withdrawal_hold.sql` | Withdrawal requests reserve the money when they are created. |
| `029_system_accounts.sql` | System accounts, so that money has a counterparty. |
| `030_customer_addresses.sql` | Saved pickup addresses (`customer_addresses`). |
| `031_structured_addresses.sql` | Addresses stored as their parts: `region`, `city`, `street`, `house`, `flat`, `fias_id`, `geo_lat`, `geo_lon`, `source`, plus a partial index on `fias_id` and a backfill from the old single-line format. See [`address_suggestions.md`](./address_suggestions.md). |
| `043_service_behaviors.sql` | Scripted services: `service_nodes.behavior_code` / `behavior_config`, the `domain_events` outbox, `user_service_claims`, `behavior_effects`, the `BONUSES` account and the `BONUS` transaction type. Seeds the verification service switched off. See [`service_behaviors.md`](./service_behaviors.md). |

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
* Two files share the `026_` prefix (`026_normalize_phone_numbers.sql` and `026_normalize_user_phones.sql`); both are idempotent, so the apply order between them does not matter.
* The table above skips `032`–`042`; those migrations are described in the module documents that introduced them.
* `customer_addresses.address` is kept as the display line alongside the structured columns added in `031`, so code that reads it did not have to change at once.
