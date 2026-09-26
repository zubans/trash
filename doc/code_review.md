# Code Review and Architecture Notes

## Backend Structure

```
backend/
├── handler/        # HTTP handlers grouped by domain
│   ├── public.go   # /health, /register, /login
│   ├── admin.go    # admin lists, users, settings
│   ├── profile.go  # профиль и адреса
│   ├── wallet.go   # кошелёк: пополнения и выводы
│   ├── role.go     # роли и права
│   ├── order.go    # customer/executor order endpoints
│   ├── shift.go    # shifts and GPS telemetry
│   ├── bid.go      # construction auction bids
│   ├── chat.go     # WebSocket chat
│   ├── errors.go   # writeDomainError: класс ошибки сервиса → HTTP-код
│   └── geo.go      # address suggestions (DaData) and geocoding proxy
├── service/        # Business logic
│   ├── errors.go   # сентинелы классов и DomainError
│   └── settings.go # единственное чтение system_settings
├── repository/     # Database access
│   └── db.go       # Querier, exec, runInTx, execExpectingOne, rowScanner
├── middleware/     # Auth, roles, CORS, rate limiting
├── dbconn/         # DSN и пул из окружения — общие для сервера и cmd/*
├── upload/         # Multipart file intake shared by chat, shop and releases
├── worker/         # Background workers (общий цикл в periodic.go, Group для shutdown)
├── photoproof/     # Фото-подтверждение: своя схема, хранилище и сверка
├── metrics/        # Prometheus-метрики и ops-эндпоинты
├── migrations/     # SQL schema migrations
├── router.go       # Route table: every handler registers its own routes (Register*Routes)
└── main.go         # Wiring (composition root) and server startup
```

Таблица маршрутов зафиксирована в `testdata/routes.txt`: `TestRouteTableMatchesSnapshot`
сравнивает метод, путь и число middleware каждого маршрута с этим снимком, а
`TestLegacyRootRoutesMirrorAPI` проверяет, что с `LEGACY_ROOT_ROUTES` у каждого
`/api/X` есть двойник `X`.

## Правила слоёв

Ревью архитектуры зафиксировало пять правил. Они соблюдены в коде и проверяются
при следующем ревью в первую очередь.

1. **Обработчик не держит репозиториев.** В структурах `handler/*.go` нет полей
   типа `repository.*Repository`: обработчик разбирает запрос, зовёт сервис и
   пишет ответ. Типы репозитория (`repository.OrdersFilter`,
   `repository.PageRequest`, `repository.Transaction`) в обработчике — это DTO
   запроса и ответа, а не доступ к базе. Доменных правил и проверок роли строкой
   в обработчиках нет: роль решает middleware (`RequireRole`,
   `RequireAdminPanel`, `RequirePermission`), допуск — сервис.
2. **Ошибки — сентинелы и `DomainError`.** Сервис не возвращает `errors.New` с
   текстом, по которому обработчику пришлось бы матчить: у ошибки есть класс
   (`repository.ErrNotFound`, `service.ErrForbidden`, `repository.ErrConflict`,
   `service.ErrOrderState`, `service.ErrRule`, `repository.ErrInsufficientFunds`,
   `service.ErrValidation`, `service.ErrNotConfigured`) и текст для человека.
   `handler.writeDomainError` отвечает по классу через `errors.Is`: 404 / 403 /
   409 / 422 / 503, а всё неизвестное — `500 internal error` с записью в лог и
   без внутреннего текста наружу. Catch-all, отдававший 422 (или 400) с
   `err.Error()` на любую ошибку, включая ошибку базы, убран. Голый
   `errors.New` в сервисе допустим только для сбоя, который и должен стать
   `500`. Свои обёртки поверх `writeDomainError`, сохраняющие прежний контракт,
   есть у каталога услуг (`writeCatalogError`: ввод — `400`), подсказок адресов
   (`writeGeoError`: `503`/`429`, прочее — `422`) и регистрации/подтверждения
   почты в `handler/public.go`.
3. **Настройки читаются одним способом.** `service/settings.go`: `settingsMap`
   читает `system_settings` один раз на операцию, `float`/`int`/`bool` считают
   записанный ноль значением, а `positiveFloat`/`positiveInt` — «не задано».
   Своих ридеров настроек у сервисов нет (у модуля `photoproof` — свой
   `Service.setting`: он отдельный пакет со своими целыми настройками).
4. **Периодические задачи — через `worker/periodic.go`.** Свой
   `for range ticker.C` внутри сервиса или в `main` не допускается: `periodic`
   даёт остановку по контексту, остановку тикера, защиту лидера, метрику прохода
   и перехват паники, а `worker.Group` позволяет `main` дождаться начатых
   проходов при выключении.
5. **Окружение читается только в `main.go` и `dbconn`.** Пути к загрузкам и
   релизам (`UPLOADS_DIR`, `RELEASES_DIR`), SMTP, `CORS_ORIGIN`,
   `AUTH_CACHE_TTL_SEC`, TTL кэшей читаются один раз при сборке и передаются
   зависимостями; параметры базы (`DB_*`, включая `DB_SSLMODE`) — в
   `dbconn.FromEnv`, общем для сервера и `cmd/*`. Провайдер адресов получает
   `service.DaDataConfig`, запасной радиус взятия `ACCEPT_RADIUS_KM` приходит
   в `OrderService` и `ExecutorGeoService` через `WithAcceptRadiusFallback`.
   Вне `main.go`, `dbconn` и `cmd/*` окружение не читается.

Транзакции: границу открывает сервис. Проверки, решающие исход (лимиты,
наличие pending-заявки, состояние смены), выполняются внутри той же
транзакции, что и запись; переход состояния идёт через
`repository.execExpectingOne`, который отдаёт `ErrConflict`, когда охрана в
`WHERE` не совпала.

## Recent Refactoring

* Moved public handlers (`Health`, `Register`, `Login`) from `backend/handler.go` into `backend/handler/public.go`.
* Renamed `Handler` → `PublicHandler` and `NewHandler` → `NewPublicHandler` to avoid ambiguity with the `handler` package.
* All handlers now live in a single package, but logically split by file/domain.
* Разрезаны god-объекты: `OrderService` отдал жизненный цикл `OrderLifecycle`,
  споры — `DisputeService`, сборку карточки — `OrderView` и `MapOrderView`,
  данные на проверку — `OrderSubmissions`; `AdminService` — профиль
  (`ProfileService`), кошелёк (`WalletService`), почту (`Mail`), инциденты
  (`MoneyIncidents`), каталог ачивок (`AchievementCatalog`), подарки
  (`GiftCatalog`), каталог услуг (`ServiceCatalog` / `ServiceCatalogAdmin`),
  релизы (`AppReleases`); `ShopService` — `Shop`, `ShopCatalog`, `ShopOrders`,
  `PerkGrants`. `AdminRepository` на 22 метода разошёлся по доменам:
  `AdminUserRepository`, `PayoutRepository`, `TransactionJournalRepository`,
  `AdminOrderRepository`, `ShiftMonitorRepository`.
* Общее вместо копий: `repository/db.go` (`exec`, `runInTx`, `execExpectingOne`,
  `rowScanner`, `idList`), `service/settings.go`, `service/outbox.go`
  (`outboxConsumer` — один потребитель outbox на диспетчеры поведений и ачивок),
  `service/script_registry.go` (`scriptRegistry`, `syncAll` пропускает
  скрипты с неизменившимся хэшем), `service/subjects.go` (`subjectLoader`),
  `worker/periodic.go`, `dbconn/`, `upload/`.

## Architectural Observations

### Strengths
* Clear separation between handler, service, and repository layers.
* Repository interfaces enable unit testing with mocks.
* Background workers are isolated in the `worker` package.
* Migrations are versioned and applied automatically in Docker.

### Areas for Improvement
1. ~~**Admin handler overload:** `AdminHandler` mixes user management, finances, settings, and logout.~~ **Resolved.** Профиль и адреса ушли в `ProfileHandler`, кошелёк — в `WalletHandler`, роли и права — в `RoleHandler`, почта — в `MailHandler`; в `AdminHandler` остались списки панели, пользователи и настройки.
2. ~~**Middleware dependency on `AdminService`:** `AuthMiddleware` receives `AdminService` only for token revocation checks.~~ **Resolved.** `NewAuthMiddleware` принимает `SessionChecker` — интерфейс с одним `IsAccessTokenRevoked`, которому удовлетворяет `AuthService`.
3. ~~**Geocoding:** Nominatim is used without rate-limiting or fallback.~~ **Resolved.** Address entry now runs on DaData (`GET /geo/suggest`), which returns coordinates with the suggestion; the geo endpoints are behind a 30 req/min limiter, and geocoding results are cached in `geocoding_cache`. Free-form strings are resolved by `AddressSuggester.Resolve` (cache, then the best DaData suggestion); Nominatim is gone. See [`address_suggestions.md`](./address_suggestions.md).
4. **Currency display:** Currency symbol is computed on the frontend based on `authStore.currency`. Consider returning the symbol from the backend or using a formatting library.
5. **Error handling:** класс ошибки и код ответа стандартизованы (`service/errors.go` + `handler/errors.go`, см. «Правила слоёв»), внутренние тексты наружу не уходят. Остаётся формат тела: ответ — это `text/plain` от `http.Error`, а не JSON с полем `error` и кодом ошибки.
6. **Logging:** Add structured logging instead of `log.Printf` for observability.
7. **Input validation:** Password complexity checks are still missing. Phone numbers are normalised to `+7XXXXXXXXXX` (migrations `026_*`), and addresses are validated by content — settlement, street and building must be present (`service.Address.Validate`) — rather than against a fixed spelling.
8. **Tests:** Increase coverage for repository and integration layers.

## Notable Decisions

* `system_settings.value` was changed from `NUMERIC` to `VARCHAR` to support non-numeric settings like `currency`.
* `customer_profiles.address` was changed from `JSONB` to `VARCHAR` to store a single pickup address. Migration `031` then split the address back into columns (`city`, `street`, `house`, `flat`, `fias_id`, coordinates), keeping `address` as the display line only: the previous single-line format was parsed with a regex that rejected ordinary house numbers such as `12к1`.
* Address suggestions have **no fallback provider** on purpose. A missing `DADATA_API_KEY` answers `503` instead of silently degrading to a source that has no apartment data.
* Mobile app uses `CapacitorHttp` to bypass WebView CORS and cleartext restrictions.
* Docker Compose uses a named volume for PostgreSQL data persistence.
