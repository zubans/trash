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

- **Основной домен**: `moya-usluga.ru`
- **Системный ящик отправителя**: `system@moya-usluga.ru`
- **Хранение писем и авторизационной БД (Persistent Volume)**: `maddy_data` (сохраняется при перезапусках контейнеров).
- **Конфигурация**: [`maddy.conf`](file:///Users/sergeyzyuban/projects/trash/maddy.conf)
- **Порты**: `25` (SMTP), `587` (Submission), `143` (IMAP).

---

## 3. Переменные окружения и Конфигурация

| Переменная | Описание | Значение по умолчанию |
| :--- | :--- | :--- |
| `ACCEPT_RADIUS_KM` | Радиус (в км), в пределах которого исполнитель может взять заказ в работу | `0.5` (500 м) |
| `UPLOADS_DIR` | Директория монтирования persistent volume для хранения изображений и файлов чата | `/app/uploads` |
| `RELEASES_DIR` | Директория для хранения релизных APK файлов | `releases` |
| `SMTP_HOST` | Хост почтового сервера | `mailserver` |
| `SMTP_PORT` | Порт подключения SMTP Submission | `587` |
| `SMTP_USER` | Почтовый адрес авторизации | `system@moya-usluga.ru` |
| `SMTP_PASSWORD` | Пароль системного почтового ящика | `systempass123` |
| `SMTP_FROM` | Адрес отправителя системных писем | `system@moya-usluga.ru` |
| `APP_BASE_URL` | Базовый URL приложения для генерации ссылок подтверждения | `https://moya-usluga.ru:8443` |
| `JWT_SECRET` | Секретный ключ подписи JWT-токенов | — |

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
