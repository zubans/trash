import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createVuestic } from 'vuestic-ui'
import 'vuestic-ui/dist/vuestic-ui.css'
import 'leaflet/dist/leaflet.css'
import App from './App.vue'
import router from './router'
import { i18n } from './i18n'

const app = createApp(App)

// Prevent unhandled Vue runtime errors from causing white screen crashes
app.config.errorHandler = (err, instance, info) => {
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
