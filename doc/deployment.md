# Инфраструктура, деплой и CI/CD (Infrastructure & Deployment)

Документ описывает окружения развертывания, Docker-конфигурацию и автоматические процессы CI/CD.

---

## 1. Среды развертывания

1. **Среда развертывания**:
   - Единая конфигурация через `docker-compose.yml`.
   - PostgreSQL на порту `5432`.
   - Backend на портах `8088` (HTTPS/HTTP) и `8089` (Cleartext/Mobile HTTP).
   - Nginx Reverse Proxy на порту `8443` (HTTPS).
   - Frontend (Vite dev server) локально на порту `5173`.

---

## 2. Почтовый сервер Maddy Mail Server (`maddy.conf`)

Для автоматической отправки системных писем (верификации регистраций, смены почты и восстановления паролей) в стек добавлен контейнер **Maddy Mail Server** (`foxcpp/maddy:0.7.0`).

Maddy **не хранит почту** для `moya-usluga.ru` и не обрабатывает его как локальный домен. Вместо этого он принимает письма от backend через порт `587` и отправляет их **напрямую** на MX-серверы получателей от имени `mail.moya-usluga.ru`.

- **Хост Maddy (HELO/EHLO)**: `mail.moya-usluga.ru`
- **Внутренний домен Maddy**: `healthlogin.local` (не `moya-usluga.ru`)
- **Системный ящик отправителя**: `system@moya-usluga.ru`
- **Авторизационная БД для backend (Persistent Volume)**: `maddy_data` (сохраняется при перезапусках контейнеров).
- **Конфигурация**: [`maddy.conf`](../maddy.conf)
- **Порты**: `25` (SMTP, входящие соединения отклоняются), `587` (Submission для backend), `143` (IMAP, не используется).

### Настройка учётной записи для backend

После первого запуска (или при смене пароля) в Maddy должна существовать учётная запись, под которой backend авторизуется для отправки почты. Создать/обновить её можно командой:

```bash
docker compose exec mailserver /bin/maddy -config /data/maddy.conf creds create system@moya-usluga.ru
```

Пароль должен совпадать со значением `SMTP_PASSWORD` в `.env` (по умолчанию `systempass123` для локальной разработки; в проде обязательно задайте свой).

---

## 3. Переменные окружения и Конфигурация

| Переменная | Описание | Значение по умолчанию |
| :--- | :--- | :--- |
| `ACCEPT_RADIUS_KM` | Радиус (в км), в пределах которого исполнитель может взять заказ в работу | `0.5` (500 м) |
| `UPLOADS_DIR` | Директория монтирования persistent volume для хранения изображений и файлов чата | `/app/uploads` |
| `RELEASES_DIR` | Директория для хранения релизных APK файлов | `releases` |
| `SMTP_HOST` | Хост внутреннего почтового сервера (Maddy), к которому подключается backend | `mailserver` |
| `SMTP_PORT` | Порт подключения SMTP Submission к Maddy | `587` |
| `SMTP_USER` | Логин backend во внутреннем Maddy | `system@moya-usluga.ru` |
| `SMTP_PASSWORD` | Пароль backend во внутреннем Maddy | `systempass123` |
| `SMTP_FROM` | Адрес отправителя системных писем | `system@moya-usluga.ru` |
| `APP_BASE_URL` | Базовый URL приложения для генерации ссылок подтверждения | `https://moya-usluga.ru:8443` |
| `JWT_SECRET` | Секретный ключ подписи JWT-токенов | — |
| `DADATA_API_KEY` | Ключ API подсказок адресов DaData. **Обязателен** — без него контейнер бэкенда не стартует, а `/geo/suggest` и `/geo/autocomplete` отвечали бы `503`. См. [`address_suggestions.md`](./address_suggestions.md) | — |
| `APP_ENDPOINTS_KEY` | Ключ доступа к списку резервных endpoint'ов (`GET /api/app/endpoints`, заголовок `X-App-Key`). **Обязателен.** Должен совпадать с `Secrets.APP_KEY` в Android-сборке | — |
| `APP_ENDPOINTS_ENC_KEY` | Ключ AES-256-GCM (32 байта = 64 hex-символа) для шифрования списка endpoint'ов. **Обязателен.** Должен совпадать с `Secrets.ENC_KEY_HEX` | — |
| `APP_ENDPOINTS_FILE` | Путь к plaintext-файлу списка внутри контейнера | `/app/vless-endpoints.json` |

---

## 3.1. Монтируемые файлы и тома

| Источник | Точка монтирования | Назначение |
| :--- | :--- | :--- |
| `./releases` | `backend:/app/releases` | Релизные APK. |
| `uploads_data` | `backend:/app/uploads` | Вложения чата и изображения. |
| `./certs` | `backend:/app/certs:ro`, `nginx:/etc/nginx/certs:ro` | TLS-сертификаты. |
| `./vless-endpoints.json` | `backend:/app/vless-endpoints.json:ro` | Список резервных VLESS-endpoint'ов. Правка файла применяется на следующем опросе приложения — пересборка не нужна. См. [`mobile_fallback_channel.md`](./mobile_fallback_channel.md). |

Раньше `vless-endpoints.json` раздавался nginx'ом как статика по `/app/vless.json`; этот том удалён — список теперь отдаёт только бэкенд, зашифрованным и за ключом доступа.

---

## 4. Автоматизация CI/CD (GitHub Actions)

В репозитории настроен автоматический Workflow (`.github/workflows/deploy.yml`):

1. **Trigger**: Push в ветку `main`.
2. **Build**:
   - Сборка Docker-образов бэкенда (`healthlogin-backend`) и фронтенда (`healthlogin-frontend`).
   - Пуш собранных образов в Docker Hub.
3. **Deploy**:
   - Подключение к целевому серверу по SSH.
   - Выполнение `docker compose pull` и `docker compose up -d --build`.
   - Автоматический запуск SQL-миграций БД при старте бэкенда.
