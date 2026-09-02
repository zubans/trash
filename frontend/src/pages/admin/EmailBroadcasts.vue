<template>
  <div class="email-broadcasts">
    <va-card>
      <va-card-title>
        <va-icon name="campaign" class="mr-2" />
        Рассылка почтовых сообщений по пользователям
      </va-card-title>
      <va-card-content>
        <p class="text--secondary mb-4">
          Отправка рекламных и информационных писем от имени системного ящика <strong>system@moya-usluga.ru</strong>.
        </p>

        <!-- Кнопки выбора сегмента / вкладки -->
        <div class="segment-selector mb-4">
          <label class="d-block font-weight-bold mb-2">Выбор аудитории для рассылки:</label>
          <div class="d-flex flex-wrap gap-2">
            <va-button
              :preset="targetGroup === 'CUSTOMERS' ? 'primary' : 'outline'"
              icon="person"
              @click="targetGroup = 'CUSTOMERS'"
            >
              Заказчики (Customer)
            </va-button>
            <va-button
              :preset="targetGroup === 'EXECUTORS' ? 'primary' : 'outline'"
              icon="engineering"
              @click="targetGroup = 'EXECUTORS'"
            >
              Исполнители (Executor)
            </va-button>
            <va-button
              :preset="targetGroup === 'CUSTOM_EMAILS' ? 'primary' : 'outline'"
              icon="contacts"
              @click="targetGroup = 'CUSTOM_EMAILS'"
            >
              Рекламные / Произвольные клиенты
            </va-button>
          </div>
        </div>

        <!-- Поле произвольного списка адресов, когда выбрана цель CUSTOM_EMAILS -->
        <div v-if="targetGroup === 'CUSTOM_EMAILS'" class="mb-4">
          <va-input
            v-model="customEmailsInput"
            type="textarea"
            label="Список email клиентов для рекламной рассылки"
            placeholder="client1@example.com, client2@domain.ru (через запятую, пробел или с новой строки)"
            :autosize="true"
            :min-rows="3"
            class="w-100"
          />
          <span class="text--secondary text-small mt-1 d-block">
            Введите почтовые адреса рекламных клиентов через запятую или с новой строки.
          </span>
        </div>

        <!-- Тема письма -->
        <div class="mb-4">
          <va-input
            v-model="subject"
            label="Тема письма"
            placeholder="Специальное предложение от Moya-Usluga.ru!"
            class="w-100"
          />
        </div>

        <!-- Редактор HTML-тела -->
        <div class="mb-4">
          <label class="d-block font-weight-bold mb-2">Текст письма (HTML поддерживается):</label>
          <va-input
            v-model="bodyHTML"
            type="textarea"
            placeholder="<p>Здравствуйте!</p><p>У нас для вас хорошие новости...</p>"
            :autosize="true"
            :min-rows="8"
            class="w-100"
          />
        </div>

        <!-- Кнопка действия и статус -->
        <div class="d-flex align-items-center justify-between mt-4">
          <va-button
            color="primary"
            icon="send"
            :loading="sending"
            :disabled="!isValid"
            @click="sendBroadcast"
          >
            Запустить рассылку
          </va-button>
        </div>

        <!-- Уведомление о результатах отправки -->
        <va-alert v-if="result" :color="result.failed === 0 ? 'success' : 'warning'" class="mt-4">
          <template #title>Результаты рассылки</template>
          <div>Успешно отправлено: <strong>{{ result.successful }}</strong> из {{ result.total }} писем.</div>
          <div v-if="result.failed > 0" class="mt-2">
            <strong>Ошибки доставки ({{ result.failed }}):</strong>
            <ul class="pl-3 mb-0 mt-1">
              <li v-for="(fail, idx) in result.failures" :key="idx">{{ fail }}</li>
            </ul>
          </div>
        </va-alert>

        <!-- Общее уведомление об ошибке -->
        <va-alert v-if="error" color="danger" class="mt-4" dismissible @dismiss="error = ''">
          {{ error }}
        </va-alert>
      </va-card-content>
    </va-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import api from '../../services/api'

const targetGroup = ref<'CUSTOMERS' | 'EXECUTORS' | 'CUSTOM_EMAILS'>('CUSTOMERS')
const customEmailsInput = ref('')
const subject = ref('')
const bodyHTML = ref('<div style="font-family: Arial, sans-serif; padding: 20px; border: 1px solid #e2e8f0; border-radius: 10px;">\n  <h2 style="color: #4f46e5;">Уважаемый клиент!</h2>\n  <p>Рады сообщить о новых возможностях сервиса moya-usluga.ru</p>\n</div>')

const sending = ref(false)
const error = ref('')
const result = ref<{
  total: number
  successful: number
  failed: number
  failures?: string[]
} | null>(null)

const parsedCustomEmails = computed(() => {
  return customEmailsInput.value
    .split(/[\s,\n;]+/)
    .map(e => e.trim())
    .filter(e => e.length > 0)
})

const isValid = computed(() => {
  if (!subject.value.trim() || !bodyHTML.value.trim()) return false
  if (targetGroup.value === 'CUSTOM_EMAILS' && parsedCustomEmails.value.length === 0) return false
  return true
})

async function sendBroadcast() {
  if (!isValid.value) return
  sending.value = true
  error.value = ''
  result.value = null

  try {
    const payload: any = {
      target_group: targetGroup.value,
      subject: subject.value,
      body_html: bodyHTML.value
    }
    if (targetGroup.value === 'CUSTOM_EMAILS') {
      payload.custom_emails = parsedCustomEmails.value
    }

    const response = await api.post('/admin/broadcast-email', payload)
    result.value = response.data
  } catch (err: any) {
    error.value = err.response?.data || err.message || 'Произошла ошибка при выполнении рассылки'
  } finally {
    sending.value = false
  }
}
</script>

<style scoped>
.gap-2 {
  gap: 0.5rem;
}
.w-100 {
  width: 100%;
}
@media (max-width: 768px) {
  .email-broadcasts {
    padding: 0;
  }
}
</style>
