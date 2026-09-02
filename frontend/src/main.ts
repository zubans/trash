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
import App from './App.vue'
import router from './router'
import { i18n } from './i18n'

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

app.mount('#app')
