# Database Migrations

## Overview
The project uses PostgreSQL with `uuid-ossp` for generating UUIDs. The migrations are split into two files:

1. **001_create_enums.sql** – defines all ENUM types used by the schema.
2. **002_create_tables.sql** – creates tables, constraints and indexes.

Run them in order (e.g., with `psql` or a migration tool) to initialise the database.

## 001_create_enums.sql
Creates ENUM types for user roles, statuses, order volume, tariffs, etc. These are referenced throughout the schema.

## 002_create_tables.sql
Creates tables for:
- `users` (includes `password` for hashed authentication credentials)
- `customer_profiles`
- `executor_profiles`
- `orders`
- `shifts`
- `transactions`
- `bids`
- `chats`
- `messages`
- `system_settings`

All foreign keys use `ON DELETE` rules to maintain referential integrity.

## How to run
```bash
# Ensure uuid-ossp extension is available
psql -d yourdb -c "CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";"

# Apply migrations
psql -d yourdb -f trash/migrations/001_create_enums.sql
psql -d yourdb -f trash/migrations/002_create_tables.sql
```

Adjust paths according to your environment.

## Notes
- All timestamps use `TIMESTAMP WITH TIME ZONE` for consistency.
- `balance` and other numeric columns use `NUMERIC(18,2)` to avoid floating‑point errors.
- `system_settings` stores decimal values; change precision if needed.
- The `users.password` column stores a bcrypt hash; plain text must never be persisted.
- The backend signs JWTs with the secret from the `JWT_SECRET` environment variable. Set a strong, unique secret in production.

---

Feel free to extend or modify these migrations to suit additional requirements.
