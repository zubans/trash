# Платформа сервиса по выносу мусора (On-Demand Waste Removal Platform)

## 1. Обзор проекта

Сервис связывает клиентов с исполнителями для выноса мусора по вызову. Система автоматически подбирает курьеров на смене с учётом геолокации, контролирует выполнение заказов и проводит взаиморасчеты.

---

## 2. Технологический стек

- **Бэкенд:** Go 1.26 (Chi Router, JWT, PostgreSQL SQL-driver)
- **База данных:** PostgreSQL 16
- **Фронтенд:** Vue 3, Vite, Pinia, Vuestic UI, vue-i18n
- **Мобильное приложение:** Capacitor 6 (Android)
- **Контейнеризация и CI/CD:** Docker Compose, Nginx, GitHub Actions

---

## 3. Модули системы и документация

Детальная техническая документация по компонентам платформы:

| Модуль / Направление | Описание | Ссылка на документацию |
| :--- | :--- | :--- |
| **Авторизация и роли** | JWT, разграничение прав (Customer, Executor, Admin), blacklist токенов. | [`auth_service.md`](./auth_service.md) |
| **Биллинг и тарифы** | Внутренние кошельки, холдирование, выплаты, штрафы и коэффициенты тарифов. | [`financial_system.md`](./financial_system.md) |
| **Жизненный цикл заказов** | Статусы (`SEARCHING`, `ASSIGNED`, `COMPLETED`, `CANCELED`) и SLA. | [`order_lifecycle.md`](./order_lifecycle.md) |
| **Аукцион строительного мусора** | Торги по заказам категории `CONSTRUCTION`, размещение и приём ставок. | [`bids_and_auction.md`](./bids_and_auction.md) |
| **Геолокация и мэтчинг** | Подбор исполнителей в радиусе 2 км, геозоны (`CIRCLE`/`POLYGON`), учет смен. | [`geolocation_and_matching.md`](./geolocation_and_matching.md) |
| **Подсказки адресов (DaData)** | Интеграция с DaData, валидация и структурирование адресов (ФИАС, координаты). | [`address_suggestions.md`](./address_suggestions.md) |
| **Чат в реальном времени** | WebSocket-чат между заказчиком и исполнителем, блокировки и события. | [`chat.md`](./chat.md) |
| **Кастомные услуги (Starlark)** | Скриптовая логика нестандартных услуг, обработка событий и сайд-эффектов. | [`service_behaviors.md`](./service_behaviors.md) |
| **Панель администратора** | Управление пользователями, заявками на вывод средств и системными параметрами. | [`admin_panel.md`](./admin_panel.md) |
| **Автообновление (Android)** | In-App обновления с нативным прогрессом загрузки пакета. | [`app_update.md`](./app_update.md) |
| **Рейтинги исполнителей** | Двусторонний рейтинг (байесовское среднее) и переход заказа в `EXECUTED`. | [`rating_system_and_order_lifecycle.md`](./rating_system_and_order_lifecycle.md) |
| **Карта и геозоны** | Интерактивная карта Leaflet, радиусы 2/10 км, 10-минутный кулдаун и аномалии. | [`executor_map_and_geofencing.md`](./executor_map_and_geofencing.md) |
| **Инфраструктура и деплой** | Docker-окружение, конфигурация Nginx и автоматический деплой через CI/CD. | [`deployment.md`](./deployment.md) |
| **Мониторинг и метрики** | Метрики бэкенда, стек Prometheus/Grafana/Alertmanager и экспортеры. | [`monitoring.md`](./monitoring.md) |
| **Миграции БД** | Перечень SQL-миграций и регламент их применения. | [`migration.md`](./migration.md) |

---

## 4. Быстрый запуск

### Локальный запуск (Docker Compose)
```bash
docker compose up -d --build
```

После запуска сервисы доступны по адресам:
- **Веб-клиент:** `https://localhost:8443`
- **Бэкенд API:** `http://localhost:8080/api`
- **Мобильный порт (HTTP Cleartext):** `http://localhost:8089`
