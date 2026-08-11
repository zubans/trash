# Инфраструктура, деплой и CI/CD (Infrastructure & Deployment)

Документ описывает окружения развертывания, Docker-конфигурацию и автоматические процессы CI/CD.

---

## 1. Среды развертывания

1. **Локальное окружение (Development)**:
   - Запуск через `docker-compose.yml` или `make dev`.
   - PostgreSQL на порту `5432`.
   - Backend на порту `8080`.
   - Frontend (Vite dev server) на порту `5173`.

2. **Продакшен окружение (Production)**:
   - Запуск через `docker-compose.prod.yml`.
   - Nginx Reverse Proxy на порту `8443` (HTTPS) и `8080` (HTTP).
   - Backend в изолированной сети.
   - Мобильный портал (Cleartext/Mobile HTTP) на порту `8089`.

---

## 2. Автоматизация CI/CD (GitHub Actions)

В репозитории настроен автоматический Workflow (`.github/workflows/deploy.yml`):

1. **Trigger**: Push в ветку `main`.
2. **Build**:
   - Сборка Docker-образов бэкенда (`healthlogin-backend`) и фронтенда (`healthlogin-frontend`).
   - Пуш собранных образов в Docker Hub.
3. **Deploy**:
   - Подключение к целевому серверу по SSH.
   - Выполнение `docker compose pull` и `docker compose up -d`.
   - Автоматический запуск SQL-миграций БД при старте бэкенда.
