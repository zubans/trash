# Этап 2: Панель Администратора и Финансы (MVP) — Спецификация реализации

В данном документе приведено детальное описание архитектуры, структуры файлов, API эндпоинтов и алгоритмов работы панели администратора и финансового модуля MVP.

---

## 1. Архитектура и расположение файлов

Реализация разделена на бэкенд-модули на Go и фронтенд-приложение на Vue 3 с использованием Vuestic Admin.

### Структура каталогов бэкенда (`backend/`)
```
backend/
├── main.go                     # Регистрация новых эндпоинтов администрирования
├── handler.go                  # Инициализация хендлеров
├── handler/
│   └── admin.go                # [NEW] HTTP-обработчики для административных эндпоинтов
├── service/
│   ├── auth.go
│   └── admin.go                # [NEW] Бизнес-логика управления пользователями и финансами
├── repository/
│   ├── user.go
│   ├── admin.go                # [NEW] SQL-запросы для модерации и баланса
│   └── settings.go             # [NEW] SQL-запросы для чтения/записи system_settings
└── middleware/
    └── auth.go                 # [NEW] Middleware для проверки JWT и роли ADMIN
```

### Структура каталогов фронтенда (`frontend/admin/`)
В качестве основы используется клонированный шаблон `vuestic-admin`.
```
frontend/admin/
├── src/
│   ├── main.ts                 # Точка входа Vue
│   ├── router/
│   │   └── index.ts            # Настройка путей и Navigation Guards (проверка токена)
│   ├── stores/
│   │   └── auth-store.ts       # Хранение JWT-токена и профиля админа (Pinia)
│   ├── services/
│   │   └── api.ts              # Инстанс Axios с интерцептором для подстановки Bearer Token
│   ├── pages/
│   │   ├── auth/
│   │   │   └── Login.vue       # Кастомизированный экран входа для администраторов
│   │   ├── users/
│   │   │   └── UserList.vue    # Таблица пользователей (<va-data-table>) с фильтрами
│   │   ├── finances/
│   │   │   ├── TopUpRequests.vue # Заявки на пополнение баланса (<va-modal>)
│   │   │   └── TransactionHistory.vue # История всех транзакций
│   │   └── settings/
│   │       └── SystemSettings.vue # Настройки коэффициентов и цен (<va-form>)
```

---

## 2. Спецификация API (Бэкенд-эндпоинты)

Все эндпоинты администрирования должны быть защищены middleware авторизации и возвращать данные в формате JSON.

| Метод | Эндпоинт | Описание | Требуемый Scope |
| :--- | :--- | :--- | :--- |
| **GET** | `/admin/users` | Получить список пользователей (с пагинацией, фильтром по роли/статусу и поиском) | `ADMIN` |
| **POST** | `/admin/users/{id}/status` | Заблокировать или разблокировать пользователя | `ADMIN` |
| **GET** | `/admin/finances/topups` | Получить список заявок на пополнение баланса | `ADMIN` |
| **POST** | `/admin/finances/topups/{id}/approve` | Подтвердить ручную заявку на пополнение баланса | `ADMIN` |
| **GET** | `/admin/transactions` | Получить общую историю транзакций | `ADMIN` |
| **GET** | `/admin/settings` | Получить текущие настройки системы (цены, штрафы) | `ADMIN` |
| **POST** | `/admin/settings` | Обновить системные настройки | `ADMIN` |

---

## 3. Слой данных и SQL-запросы

Слой репозитория (`repository/admin.go`) оперирует транзакциями SQL для предотвращения нарушений целостности данных.

### Схема таблиц для финансовых заявок (добавление в миграции)
```sql
-- Статус заявки на пополнение
CREATE TYPE topup_status AS ENUM ('PENDING', 'APPROVED', 'REJECTED');

-- Заявки на ручное пополнение баланса
CREATE TABLE IF NOT EXISTS balance_topup_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    status topup_status NOT NULL DEFAULT 'PENDING',
    admin_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE
);
```

### Основные SQL-запросы

1. **Транзакционное пополнение баланса пользователя (`ApproveTopUpRequest`)**:
   ```sql
   -- Шаг 1: Проверка статуса заявки и блокировка строки от записи
   SELECT status, amount, user_id 
   FROM balance_topup_requests 
   WHERE id = $1 FOR UPDATE;

   -- Шаг 2: Обновление статуса заявки
   UPDATE balance_topup_requests 
   SET status = 'APPROVED', admin_id = $2, updated_at = now() 
   WHERE id = $1;

   -- Шаг 3: Обновление баланса пользователя (атомарное сложение с блокировкой)
   UPDATE users 
   SET balance = balance + $2 
   WHERE id = $3;

   -- Шаг 4: Запись транзакции в аудит-лог
   INSERT INTO transactions (user_id, type, amount, admin_id) 
   VALUES ($1, 'TOP_UP', $2, $3);
   ```

2. **Блокировка пользователя (`UpdateUserStatus`)**:
   ```sql
   UPDATE users 
   SET status = $1 
   WHERE id = $2;
   ```

---

## 4. Алгоритмы и логика работы

### А. Алгоритм аутентификации и авторизации (Middleware)
Для защиты административной зоны применяется middleware проверки токена.
```go
// Псевдокод алгоритма в middleware/auth.go
func AdminRequired(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 1. Получение заголовка Authorization
        authHeader := r.Header.Get("Authorization")
        if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

        // 2. Валидация подписи JWT и срока exp
        token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
            return []byte(os.Getenv("JWT_SECRET")), nil
        })
        if err != nil || !token.Valid {
            http.Error(w, "Invalid token", http.StatusUnauthorized)
            return
        }
        claims := token.Claims.(*Claims)

        // 3. Проверка роли в claims
        if claims.Role != "ADMIN" {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }

        // 4. Проверка статуса администратора в БД (защита от забаненных админов)
        user, err := userRepo.FindByID(claims.UserID)
        if err != nil || user.Status == "BANNED" {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }

        // Добавление данных пользователя в контекст запроса
        ctx := context.WithValue(r.Context(), "user", user)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

### Б. Алгоритм предотвращения «состояния гонки» (Race Condition) при пополнении баланса
При обработке ручных заявок на пополнение баланса критически важно исключить сценарий, когда модератор нажимает кнопку подтверждения дважды, либо два разных администратора обрабатывают одну заявку параллельно.

1. **Пессимистическая блокировка (`FOR UPDATE`)**:
   Запрос на получение статуса заявки выполняется с флагом блокировки `FOR UPDATE`. Это блокирует выбранную строку базы данных для любых других транзакций до тех пор, пока текущая транзакция бэкенда не завершит `COMMIT` или `ROLLBACK`.
2. **Проверка состояния конечного автомата (State Machine)**:
   Внутри транзакции проверяется, равен ли статус `PENDING`. Если статус уже изменился на `APPROVED` или `REJECTED`, транзакция немедленно откатывается (`ROLLBACK`), предотвращая повторное начисление баланса.

### В. Механизм мгновенной блокировки сессий (Token Revocation)
Поскольку JWT-токены работают в режиме stateless (без сохранения состояния на сервере) и действуют 24 часа, обычная смена статуса пользователя в БД на `BANNED` не прекратит его сессию мгновенно. 

Для MVP выбран гибридный подход:
1. **Проверка статуса при критических операциях:** 
   При каждом запросе к критически важным ресурсам (создание заказов, списание средств, выход на смену) бэкенд выполняет легкий запрос в базу данных для проверки статуса пользователя: `SELECT status FROM users WHERE id = $1`. Если статус равен `BANNED`, запрос отклоняется с кодом `403 Forbidden`, даже если JWT-токен валиден по времени.
2. **Blacklist таблица в БД (для полной блокировки токенов):**
   При блокировке пользователя администратором, хендлер записывает JWT-токен (или `jti` токена) в таблицу `revoked_tokens` со временем истечения:
   ```sql
   CREATE TABLE revoked_tokens (
       token_hash VARCHAR PRIMARY KEY,
       expires_at TIMESTAMP WITH TIME ZONE NOT NULL
   );
   ```
   При каждом запросе middleware проверяет хеш токена по этой таблице. Периодический фоновый воркер (cron-задача) очищает просроченные записи из `revoked_tokens`.

---

## 5. Фронтенд на Vuestic Admin

### Реализация Navigation Guards (`router/index.ts`)
```typescript
router.beforeEach((to, from, next) => {
  const token = localStorage.getItem('token')
  const userRole = localStorage.getItem('role') // Сохраняется при успешном логине

  // Защита админских страниц
  if (to.meta.requiresAdmin) {
    if (!token || userRole !== 'ADMIN') {
      next({ name: 'login' })
    } else {
      next()
    }
  } else {
    next()
  }
})
```

### Интеграция Axios с перехватом ошибок (`services/api.ts`)
```typescript
import axios from 'axios'

const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL || 'http://localhost:8080',
})

// Подстановка токена в каждый запрос
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Обработка автоматического разлогина при 401/403 ошибках
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response && [401, 403].includes(error.response.status)) {
      localStorage.clear()
      window.location.href = '/auth/login'
    }
    return Promise.reject(error)
  }
)

export default api
```
