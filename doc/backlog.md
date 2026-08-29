# Беклог

Отложенные работы и вынесенные из `main` подсистемы. Каждый пункт указывает,
где лежит код и как его вернуть.

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
