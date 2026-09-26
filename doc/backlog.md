# Беклог

Отложенные работы и вынесенные из `main` подсистемы. Каждый пункт указывает,
где лежит код и как его вернуть.

---

## Хвосты ревью архитектуры бэкенда

Правила слоёв — в [`code_review.md`](./code_review.md#правила-слоёв). Места,
которые им пока не соответствуют:

- **SLA-даунгрейд живёт в воркере.** `worker/sla_worker.go` (`downgradeOrder`)
  сам открывает транзакцию, пишет в `orders` сырым `UPDATE` и зовёт
  `Ledger.Release`: денежное правило — в воркере, а не в сервисе заказов.
  `AuctionWorker` тоже выбирает заказы своим SQL, но отменяет их через
  `OrderService.CancelUnclaimedAuction`.
- **`ACCEPT_RADIUS_KM` читается на пути запроса.** `acceptRadiusFromEnv` в
  `service/executor_geo.go` — запасной источник радиуса взятия для баз старше
  миграции 049. Убрать вместе с переменной, когда все установки получат
  настройку `accept_radius_km`.
- **Формат ошибок.** Код ответа идёт по классу ошибки, но тело — `text/plain`
  от `http.Error`, а не JSON с машинным кодом.
- **Автооткрытая смена в `Accept` открывается вне транзакции назначения**
  (решение принято сознательно: при сбое назначения смена закрывается обратно
  без штрафа, см. [`order_lifecycle.md`](./order_lifecycle.md#3-автооткрытие-смены-при-взятии-заказа)).

---

## Резервный VPN-канал (Xray/VLESS fallback) — вынесен из `main`

**Ветка:** `feature/vpn-proxy-fallback`
**Дата выноса:** 2026-08-29
**Причина:** канал никем не используется на исправной системе и требует
постоянного сопровождения (серверы, ключи Reality, сертификаты, отдельный
контур мониторинга). Убран из `main`, чтобы не тянуть его вес в основной ветке.

### Что это было

Прозрачный fallback-транспорт мобильного приложения: когда прямой путь до API
недоступен (блокировки провайдера), трафик уходил в VLESS-туннель через libXray,
туннелируя только хост API. Список endpoint'ов приложение забирало с
`GET /api/app/endpoints` — зашифрованным AES-256-GCM и за ключом `X-App-Key`.

### Что удалено из `main`

- **Frontend:** `frontend/src/components/VpnDebugConsole.vue` и его регистрация в `App.vue`.
- **Android:** пакет `frontend/android/.../net/*` (VlessChannel, XrayController,
  LibXrayBridge, ChannelManager, NetConfig, RemoteConfigRepo, AesGcm, Secrets, DebugLog),
  `plugins/VpnDebugPlugin.java`, libXray AAR в `app/libs/`, инъекция ключей и
  proxy-роутинг в `MainActivity.java` / `NativeWebSocketPlugin.java` / `build.gradle`.
- **Backend:** `handler/app_endpoints.go` (+тест), `cmd/vlessprobe/`, метрики
  `app_endpoints_*` в `metrics/`, VPN-источник и словарь метрик в `cmd/opsbot/`.
- **Мониторинг:** дашборд `grafana/dashboards/vless.json`, правила
  `prometheus/rules/vless.yml` (+тесты), `monitoring/vlessprobe/`, scrape-job и
  алерты Vless* в Prometheus/Alertmanager.
- **Инфраструктура:** `vless-endpoints.json`(.example), тома/переменные
  `APP_ENDPOINTS_*` / `PROBE_*` в docker-compose, Makefile, CI (`deploy.yml`),
  `.env.deploy.example`, `.gitignore`; документ `doc/mobile_fallback_channel.md`.

### Как вернуть

Код целиком сохранён в ветке — вычитать конкретные файлы или влить обратно:

```bash
git checkout feature/vpn-proxy-fallback -- <путь>   # вернуть отдельные файлы
# либо
git merge feature/vpn-proxy-fallback                # вернуть всё
```

> Примечание: ветка снята с коммита `02535258`, поэтому содержит и остальное
> состояние `main` на момент выноса. Для точечного возврата берите только
> файлы из списка выше.
