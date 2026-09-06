<template>
  <div class="gifts-wrapper">
    <div class="gifts-container">
      <div class="top-nav">
        <button type="button" class="btn-back" @click="goBack">
          <i class="ph-bold ph-arrow-left"></i>
          Вернуться на главную
        </button>
        <div class="page-header-title">
          <i class="ph-fill ph-gift icon-title"></i>
          Подарки
        </div>
      </div>

      <div v-if="loading" class="state-note">Загружаем подарки…</div>
      <div v-else-if="error" class="state-note error">{{ error }}</div>
      <div v-else-if="!gifts.length" class="state-note">
        Подарков пока нет. Они приходят за достижения — и сюда, и во внутреннюю почту.
      </div>

      <div v-else class="gift-list">
        <div v-for="gift in gifts" :key="gift.id" class="gift-card" :class="statusClass(gift)">
          <div class="gift-icon"><i :class="kindIcon(gift)"></i></div>

          <div class="gift-body">
            <div class="gift-title">{{ title(gift) }}</div>
            <div class="gift-description">{{ description(gift) }}</div>

            <div class="gift-meta">
              <span class="chip">{{ kindLabel(gift) }}</span>
              <span class="chip" :class="statusChip(gift)">{{ statusLabel(gift) }}</span>
              <span v-if="gift.expires_at" class="chip warn">
                действует до {{ formatDate(gift.expires_at) }}
              </span>
            </div>

            <!-- Купон нужен любому подарку: денежному он не нужен для получения,
                 но остаётся следом выдачи, а вещь по нему выдают на пункте. -->
            <div class="coupon-row">
              <span class="coupon-label">Купон</span>
              <code class="coupon-code">{{ gift.coupon_code }}</code>
            </div>

            <div v-if="secrets[gift.id]" class="secret-box">
              <span class="secret-label">Код подарка</span>
              <code class="secret-code">{{ secrets[gift.id] }}</code>
            </div>

            <div class="gift-actions">
              <button
                v-if="canReveal(gift)"
                type="button"
                class="btn-reveal"
                :disabled="revealing === gift.id"
                @click="reveal(gift)"
              >
                <span v-if="revealing === gift.id" class="spinner-sm"></span>
                <template v-else>Показать код</template>
              </button>
              <span v-else-if="gift.gift?.kind === 'PHYSICAL' && gift.status !== 'REDEEMED'" class="hint">
                Покажите купон на пункте выдачи.
              </span>
              <span v-else-if="gift.gift?.kind === 'BONUS'" class="hint">
                Бонус зачислен на счёт.
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { getGifts, revealGift, type UserGift } from '../../api/achievements'

const KIND_ICONS: Record<string, string> = {
  BONUS: 'ph-fill ph-wallet',
  CERTIFICATE: 'ph-fill ph-ticket',
  PHYSICAL: 'ph-fill ph-package',
  PROMO: 'ph-fill ph-tag',
}

const KIND_LABELS: Record<string, string> = {
  BONUS: 'деньги на счёт',
  CERTIFICATE: 'сертификат',
  PHYSICAL: 'подарок',
  PROMO: 'промокод',
}

const STATUS_LABELS: Record<string, string> = {
  ISSUED: 'ждёт вас',
  REVEALED: 'код открыт',
  REDEEMED: 'получен',
  EXPIRED: 'просрочен',
  CANCELED: 'отменён',
}

export default defineComponent({
  name: 'GiftsPage',
  setup() {
    const router = useRouter()

    const gifts = ref<UserGift[]>([])
    const loading = ref(true)
    const error = ref('')
    const revealing = ref('')
    // Секреты живут только в памяти вкладки: список их не отдаёт, и сохранять
    // их куда-либо на клиенте незачем.
    const secrets = ref<Record<string, string>>({})

    const load = async () => {
      loading.value = true
      error.value = ''
      try {
        gifts.value = await getGifts()
      } catch {
        error.value = 'Не удалось загрузить подарки. Попробуйте обновить страницу.'
      } finally {
        loading.value = false
      }
    }

    const canReveal = (gift: UserGift) =>
      (gift.gift?.kind === 'CERTIFICATE' || gift.gift?.kind === 'PROMO') &&
      gift.status !== 'EXPIRED' &&
      gift.status !== 'CANCELED' &&
      !secrets.value[gift.id]

    const reveal = async (gift: UserGift) => {
      revealing.value = gift.id
      try {
        const revealed = await revealGift(gift.id)
        if (revealed.secret) secrets.value[gift.id] = revealed.secret
        gift.status = revealed.status
      } catch {
        error.value = 'Не удалось показать код. Возможно, срок подарка истёк.'
      } finally {
        revealing.value = ''
      }
    }

    const title = (gift: UserGift) => gift.gift?.title?.ru || gift.gift_code
    const description = (gift: UserGift) => gift.gift?.description?.ru || ''
    const kindIcon = (gift: UserGift) => KIND_ICONS[gift.gift?.kind ?? ''] ?? 'ph-fill ph-gift'
    const kindLabel = (gift: UserGift) => KIND_LABELS[gift.gift?.kind ?? ''] ?? 'подарок'
    const statusLabel = (gift: UserGift) => STATUS_LABELS[gift.status] ?? gift.status
    const statusChip = (gift: UserGift) =>
      gift.status === 'EXPIRED' || gift.status === 'CANCELED' ? 'muted' : 'ok'
    const statusClass = (gift: UserGift) =>
      gift.status === 'EXPIRED' || gift.status === 'CANCELED' ? 'inactive' : ''

    const formatDate = (value?: string) =>
      value ? new Date(value).toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' }) : ''

    const goBack = () => router.push('/executor')

    onMounted(load)

    return {
      gifts,
      loading,
      error,
      revealing,
      secrets,
      canReveal,
      reveal,
      title,
      description,
      kindIcon,
      kindLabel,
      statusLabel,
      statusChip,
      statusClass,
      formatDate,
      goBack,
    }
  },
})
</script>

<style scoped>
.gifts-wrapper {
  min-height: 100vh;
  background: #f6f7fb;
  padding: 16px;
}

.gifts-container {
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

.btn-back {
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

.page-header-title {
  font-size: 18px;
  font-weight: 600;
  color: #111827;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.icon-title {
  color: #ec4899;
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

.gift-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.gift-card {
  background: #fff;
  border-radius: 14px;
  padding: 16px;
  display: flex;
  gap: 14px;
  border: 1px solid #eef0f4;
}

.gift-card.inactive {
  opacity: 0.6;
}

.gift-icon {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: #fdf2f8;
  color: #db2777;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 22px;
  flex-shrink: 0;
}

.gift-body {
  flex: 1;
  min-width: 0;
}

.gift-title {
  font-weight: 600;
  color: #111827;
  font-size: 15px;
}

.gift-description {
  color: #6b7280;
  font-size: 13px;
  margin-top: 4px;
}

.gift-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
}

.chip {
  font-size: 11px;
  padding: 3px 8px;
  border-radius: 999px;
  background: #f3f4f6;
  color: #4b5563;
}

.chip.ok {
  background: #ecfdf5;
  color: #059669;
}

.chip.muted {
  background: #f3f4f6;
  color: #9ca3af;
}

.chip.warn {
  background: #fef3c7;
  color: #92400e;
}

.coupon-row {
  margin-top: 12px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.coupon-label,
.secret-label {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: #9ca3af;
}

.coupon-code,
.secret-code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 14px;
  letter-spacing: 0.08em;
  color: #111827;
  background: #f3f4f6;
  padding: 4px 8px;
  border-radius: 8px;
}

.secret-box {
  margin-top: 10px;
  padding: 10px 12px;
  border-radius: 10px;
  background: #ecfdf5;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.gift-actions {
  margin-top: 12px;
}

.btn-reveal {
  border: none;
  background: #111827;
  color: #fff;
  border-radius: 10px;
  padding: 8px 16px;
  font-size: 13px;
  cursor: pointer;
}

.btn-reveal:disabled {
  opacity: 0.6;
  cursor: default;
}

.hint {
  font-size: 12px;
  color: #6b7280;
}

.spinner-sm {
  display: inline-block;
  width: 14px;
  height: 14px;
  border: 2px solid rgba(255, 255, 255, 0.4);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
