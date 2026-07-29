# Code Review and Architecture Notes

## Backend Structure

```
backend/
├── handler/        # HTTP handlers grouped by domain
│   ├── public.go   # /health, /register, /login
│   ├── admin.go    # admin, user management, settings
│   ├── order.go    # customer/executor order endpoints
│   ├── shift.go    # shifts and GPS telemetry
│   ├── bid.go      # construction auction bids
│   ├── chat.go     # WebSocket chat
│   └── geo.go      # geocoding proxy
├── service/        # Business logic
├── repository/     # Database access
├── middleware/     # Auth and role middleware
├── worker/         # Background workers
├── migrations/     # SQL schema migrations
└── main.go         # Wiring and server startup
```

## Recent Refactoring

* Moved public handlers (`Health`, `Register`, `Login`) from `backend/handler.go` into `backend/handler/public.go`.
* Renamed `Handler` → `PublicHandler` and `NewHandler` → `NewPublicHandler` to avoid ambiguity with the `handler` package.
* All handlers now live in a single package, but logically split by file/domain.

## Architectural Observations

### Strengths
* Clear separation between handler, service, and repository layers.
* Repository interfaces enable unit testing with mocks.
* Background workers are isolated in the `worker` package.
* Migrations are versioned and applied automatically in Docker.

### Areas for Improvement
1. **Admin handler overload:** `AdminHandler` currently mixes user management, finances, settings, and logout. Consider splitting into smaller handlers (e.g., `FinanceHandler`, `SettingsHandler`).
2. **Middleware dependency on `AdminService`:** `AuthMiddleware` receives `AdminService` only for token revocation checks. A dedicated `TokenRepository`-based service would reduce coupling.
3. **Geocoding:** Nominatim is used without rate-limiting or fallback. For production, consider Yandex Geocoder or caching with TTL.
4. **Currency display:** Currency symbol is computed on the frontend based on `authStore.currency`. Consider returning the symbol from the backend or using a formatting library.
5. **Error handling:** Some handlers return raw error strings in HTTP responses. Standardize error responses (e.g., JSON with `error` field and error codes).
6. **Logging:** Add structured logging instead of `log.Printf` for observability.
7. **Input validation:** Phone format, password complexity, and address validation are missing.
8. **Tests:** Increase coverage for repository and integration layers.

## Notable Decisions

* `system_settings.value` was changed from `NUMERIC` to `VARCHAR` to support non-numeric settings like `currency`.
* `customer_profiles.address` was changed from `JSONB` to `VARCHAR` to store a single pickup address.
* Mobile app uses `CapacitorHttp` to bypass WebView CORS and cleartext restrictions.
* Docker Compose uses a named volume for PostgreSQL data persistence.
