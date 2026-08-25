import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createVuestic } from 'vuestic-ui'
import 'vuestic-ui/dist/vuestic-ui.css'
import 'leaflet/dist/leaflet.css'
// Icons are bundled, not fetched from a CDN at runtime. Seven components used
// to inject <script src="https://unpkg.com/@phosphor-icons/web">, which is
// third-party code executing in the app's origin next to the session token,
// with no pinned version and no integrity check — and it took the whole icon
// set down with it whenever unpkg was unreachable. Only the weights the app
import '@phosphor-icons/web/regular/style.css'
import '@phosphor-icons/web/bold/style.css'
import '@phosphor-icons/web/fill/style.css'
import App from './App.vue'
import router from './router'
import { i18n } from './i18n'

const app = createApp(App)

// Prevent unhandled Vue runtime errors from causing white screen crashes
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
