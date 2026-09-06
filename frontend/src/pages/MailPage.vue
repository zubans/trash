<template>
  <div class="mail-wrapper">
    <div class="mail-container">
      <div class="top-nav">
        <button type="button" class="btn-back" @click="goBack">
          <i class="ph-bold ph-arrow-left"></i>
          Назад
        </button>
        <div class="page-header-title">
          <i class="ph-fill ph-envelope-simple icon-title"></i>
          Почта
          <span v-if="unread" class="unread-badge">{{ unread }}</span>
        </div>
        <button v-if="unread" type="button" class="btn-secondary" @click="readAll">
          Прочитать все
        </button>
      </div>

      <div v-if="loading" class="state-note">Загружаем почту…</div>
      <div v-else-if="error" class="state-note error">{{ error }}</div>
      <div v-else-if="!messages.length" class="state-note">
        Писем пока нет. Сюда приходят достижения, подарки, акции и новости.
      </div>

      <div v-else class="mail-list">
        <div
          v-for="message in messages"
          :key="message.id"
          class="mail-card"
          :class="{ unread: !message.read_at }"
          @click="open(message)"
        >
          <div class="mail-icon" :class="message.kind.toLowerCase()">
            <i :class="kindIcon(message.kind)"></i>
          </div>
          <div class="mail-body">
            <div class="mail-head">
              <span class="mail-subject">{{ message.subject }}</span>
              <span class="mail-date">{{ formatDate(message.created_at) }}</span>
            </div>
            <div class="mail-text">{{ message.body }}</div>
            <button
              v-if="message.ref_type === 'gift'"
              type="button"
              class="btn-link"
              @click.stop="goToGifts"
            >
              Открыть подарок
            </button>
            <button
              v-else-if="message.ref_type === 'achievement'"
              type="button"
              class="btn-link"
              @click.stop="goToAchievements"
            >
              Посмотреть достижения
            </button>
          </div>
          <button type="button" class="btn-delete" title="Удалить" @click.stop="remove(message)">
            <i class="ph ph-trash"></i>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import {
  deleteMail,
  getMail,
  markAllMailRead,
  markMailRead,
  type MailMessage,
} from '../api/achievements'

const KIND_ICONS: Record<string, string> = {
  ACHIEVEMENT: 'ph-fill ph-trophy',
  GIFT: 'ph-fill ph-gift',
  PROMO: 'ph-fill ph-megaphone',
  NEWS: 'ph-fill ph-newspaper',
  SYSTEM: 'ph-fill ph-info',
}

export default defineComponent({
  name: 'MailPage',
  setup() {
    const router = useRouter()

    const messages = ref<MailMessage[]>([])
    const unread = ref(0)
    const loading = ref(true)
    const error = ref('')

    const load = async () => {
      loading.value = true
      error.value = ''
      try {
        const inbox = await getMail()
        messages.value = inbox.messages
        unread.value = inbox.unread
      } catch {
        error.value = 'Не удалось загрузить почту. Попробуйте обновить страницу.'
      } finally {
        loading.value = false
      }
    }

    // Открытое письмо помечается прочитанным сразу в списке: ждать ответа
    // сервера, чтобы убрать точку, значит показывать её ещё секунду после того,
    // как человек уже прочитал.
    const open = async (message: MailMessage) => {
      if (message.read_at) return
      message.read_at = new Date().toISOString()
      unread.value = Math.max(0, unread.value - 1)
      try {
        await markMailRead(message.id)
      } catch {
        /* отметка о прочтении не стоит того, чтобы о ней сообщать */
      }
    }

    const readAll = async () => {
      const now = new Date().toISOString()
      messages.value.forEach((message) => {
        if (!message.read_at) message.read_at = now
      })
      unread.value = 0
      try {
        await markAllMailRead()
      } catch {
        /* см. выше */
      }
    }

    const remove = async (message: MailMessage) => {
      messages.value = messages.value.filter((item) => item.id !== message.id)
      if (!message.read_at) unread.value = Math.max(0, unread.value - 1)
      try {
        await deleteMail(message.id)
      } catch {
        await load()
      }
    }

    const kindIcon = (kind: string) => KIND_ICONS[kind] ?? KIND_ICONS.SYSTEM

    const formatDate = (value: string) => {
      const date = new Date(value)
      const today = new Date()
      if (date.toDateString() === today.toDateString()) {
        return date.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
      }
      return date.toLocaleDateString('ru-RU', { day: 'numeric', month: 'short' })
    }

    const goBack = () => router.back()
    const goToGifts = () => router.push('/executor/gifts')
    const goToAchievements = () => router.push('/executor/achievements')

    onMounted(load)

    return {
      messages,
      unread,
      loading,
      error,
      open,
      readAll,
      remove,
      kindIcon,
      formatDate,
      goBack,
      goToGifts,
      goToAchievements,
    }
  },
})
</script>

<style scoped>
.mail-wrapper {
  min-height: 100vh;
  background: #f6f7fb;
  padding: 16px;
}

.mail-container {
  max-width: 720px;
  margin: 0 auto;
}

.top-nav {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.btn-back,
.btn-secondary {
  border: none;
  background: #fff;
  border-radius: 10px;
  padding: 8px 14px;
  font-size: 14px;
  color: #4b5563;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.btn-secondary {
  margin-left: auto;
}

.page-header-title {
  font-size: 18px;
  font-weight: 600;
  color: #111827;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.icon-title {
  color: #3b82f6;
}

.unread-badge {
  background: #ef4444;
  color: #fff;
  font-size: 12px;
  border-radius: 999px;
  padding: 1px 8px;
}

.state-note {
  background: #fff;
  border-radius: 12px;
  padding: 20px;
  text-align: center;
  color: #6b7280;
  font-size: 14px;
}

.state-note.error {
  color: #b91c1c;
}

.mail-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.mail-card {
  background: #fff;
  border-radius: 12px;
  padding: 14px;
  display: flex;
  gap: 12px;
  cursor: pointer;
  border: 1px solid #eef0f4;
}

.mail-card.unread {
  border-color: #bfdbfe;
  background: #f8fbff;
}

.mail-icon {
  width: 38px;
  height: 38px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  flex-shrink: 0;
  background: #f3f4f6;
  color: #6b7280;
}

.mail-icon.achievement {
  background: #fffbeb;
  color: #d97706;
}

.mail-icon.gift {
  background: #fdf2f8;
  color: #db2777;
}

.mail-icon.promo {
  background: #eff6ff;
  color: #2563eb;
}

.mail-body {
  flex: 1;
  min-width: 0;
}

.mail-head {
  display: flex;
  justify-content: space-between;
  gap: 8px;
}

.mail-subject {
  font-weight: 600;
  color: #111827;
  font-size: 14px;
}

.mail-date {
  font-size: 12px;
  color: #9ca3af;
  white-space: nowrap;
}

.mail-text {
  color: #4b5563;
  font-size: 13px;
  margin-top: 4px;
}

.btn-link {
  border: none;
  background: none;
  color: #2563eb;
  font-size: 13px;
  padding: 6px 0 0;
  cursor: pointer;
}

.btn-delete {
  border: none;
  background: none;
  color: #d1d5db;
  cursor: pointer;
  align-self: flex-start;
  font-size: 16px;
}

.btn-delete:hover {
  color: #ef4444;
}
</style>
