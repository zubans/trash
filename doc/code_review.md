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
│   └── geo.go      # address suggestions (DaData) and geocoding proxy
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
3. ~~**Geocoding:** Nominatim is used without rate-limiting or fallback.~~ **Resolved.** Address entry now runs on DaData (`GET /geo/suggest`), which returns coordinates with the suggestion; the geo endpoints are behind a 30 req/min limiter, and geocoding results are cached in `geocoding_cache`. Nominatim is left only for geocoding free-form strings. See [`address_suggestions.md`](./address_suggestions.md).
4. **Currency display:** Currency symbol is computed on the frontend based on `authStore.currency`. Consider returning the symbol from the backend or using a formatting library.
5. **Error handling:** Some handlers return raw error strings in HTTP responses. Standardize error responses (e.g., JSON with `error` field and error codes).
6. **Logging:** Add structured logging instead of `log.Printf` for observability.
7. **Input validation:** Password complexity checks are still missing. Phone numbers are normalised to `+7XXXXXXXXXX` (migrations `026_*`), and addresses are validated by content — settlement, street and building must be present (`service.Address.Validate`) — rather than against a fixed spelling.
8. **Tests:** Increase coverage for repository and integration layers.

## Notable Decisions

* `system_settings.value` was changed from `NUMERIC` to `VARCHAR` to support non-numeric settings like `currency`.
* `customer_profiles.address` was changed from `JSONB` to `VARCHAR` to store a single pickup address. Migration `031` then split the address back into columns (`city`, `street`, `house`, `flat`, `fias_id`, coordinates), keeping `address` as the display line only: the previous single-line format was parsed with a regex that rejected ordinary house numbers such as `12к1`.
* Address suggestions have **no fallback provider** on purpose. A missing `DADATA_API_KEY` answers `503` instead of silently degrading to a source that has no apartment data.
* Mobile app uses `CapacitorHttp` to bypass WebView CORS and cleartext restrictions.
* Docker Compose uses a named volume for PostgreSQL data persistence.
