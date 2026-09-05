import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createVuestic } from 'vuestic-ui'
import 'vuestic-ui/dist/vuestic-ui.css'
import 'leaflet/dist/leaflet.css'
// Иконки идут в сборке, а не запрашиваются с CDN во время работы. Семь
// компонентов раньше внедряли <script src="https://unpkg.com/@phosphor-icons/web">,
// то есть сторонний код, выполняющийся в источнике приложения рядом с токеном
// сессии, без закреплённой версии и без проверки целостности — и он утаскивал за
// собой весь набор иконок всякий раз, когда unpkg был недоступен. Только те начертания, что приложение
import '@phosphor-icons/web/regular/style.css'
import '@phosphor-icons/web/bold/style.css'
import '@phosphor-icons/web/fill/style.css'
import './styles/admin-table.css'
import './styles/loading.css'
import App from './App.vue'
import router from './router'
import { i18n } from './i18n'
import { setSessionExpiredHandler, startSessionWatch } from './services/api'
import { useAuthStore } from './stores/auth-store'

const app = createApp(App)

// Не даём необработанным runtime-ошибкам Vue приводить к белому экрану
app.config.errorHandler = (err, _instance, info) => {
  console.error('[Vue ErrorHandler Caught]:', err, info)
}

window.addEventListener('error', (event) => {
  console.error('[Global Window Error]:', event.error || event.message)
})

window.addEventListener('unhandledrejection', (event) => {
  console.error('[Global Unhandled Rejection]:', event.reason)
})

app.use(createPinia())
app.use(router)
app.use(i18n)
app.use(createVuestic())

// Конец сессии обязан дойти и до хранилища авторизации: оно живёт в памяти и
// продолжало бы считать пользователя вошедшим после того, как обновление
// провалилось. Тогда навигационный гард возвращает его с /login обратно в
// кабинет, и получается открытый кабинет, где каждое действие отвечает ошибкой
// авторизации, — ровно то, что видно в мобильном приложении.
setSessionExpiredHandler(() => {
  useAuthStore().logout()
  router.replace('/login').catch(() => {
    // Навигация может быть прервана параллельным переходом — вход всё равно
    // остаётся единственным доступным экраном, потому что стор уже пуст.
  })
})

// Планирует обновление для сессии, восстановленной из localStorage, и обновляет
// её при возврате приложения из фона, где таймеры WebView не идут.
startSessionWatch()

app.mount('#app')
