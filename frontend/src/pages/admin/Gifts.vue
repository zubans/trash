<template>
  <div class="gifts-admin">
    <header class="page-head">
      <div>
        <h1>Подарки</h1>
        <p class="page-sub">
          Подарки бывают четырёх родов, и различие между ними не косметическое: у
          денег есть проводка, у сертификата — пул кодов, у вещи — склад и человек,
          который её выдаёт по купону.
        </p>
      </div>
      <button type="button" class="btn-refresh" :disabled="loading" @click="load">
        <i class="ph-bold ph-arrows-clockwise"></i>
      </button>
    </header>

    <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>
    <p v-if="successMsg" class="alert success">{{ successMsg }}</p>

    <!-- Погашение купона: то, что администратор делает на пункте выдачи, и
         поэтому стоит первым, а не в конце длинного списка. -->
    <section class="panel">
      <h2>Погасить купон</h2>
      <p class="panel-sub">Введите код с экрана исполнителя, когда выдаёте вещь на руки.</p>
      <div class="row">
        <input v-model="coupon" class="input mono" placeholder="XXXX-XXXX-XXXX" />
        <button type="button" class="btn-primary" :disabled="!coupon || redeeming" @click="redeem">
          Погасить
        </button>
      </div>
      <p v-if="redeemMsg" class="hint" :class="{ error: redeemFailed }">{{ redeemMsg }}</p>
    </section>

    <section class="panel">
      <h2>Каталог</h2>
      <table class="gift-table">
        <thead>
          <tr>
            <th>Код</th>
            <th>Род</th>
            <th>Название</th>
            <th>Номинал</th>
            <th>Остаток</th>
            <th>Срок</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="gift in gifts" :key="gift.code">
            <td class="mono">{{ gift.code }}</td>
            <td>{{ kindLabel(gift.kind) }}</td>
            <td>{{ gift.title?.ru || '—' }}</td>
            <td>{{ gift.kind === 'BONUS' ? formatAmount(gift.amount) : '—' }}</td>
            <td>{{ stockLabel(gift) }}</td>
            <td>{{ gift.valid_days ? gift.valid_days + ' дн.' : 'бессрочно' }}</td>
            <td>
              <span class="badge" :class="gift.is_active ? 'ok' : 'muted'">
                {{ gift.is_active ? 'активен' : 'выключен' }}
              </span>
              <button
                v-if="gift.kind === 'CERTIFICATE'"
                type="button"
                class="btn-link"
                @click="openCodes(gift.code)"
              >
                коды
              </button>
            </td>
          </tr>
          <tr v-if="!gifts.length">
            <td colspan="7" class="empty">Подарков пока нет.</td>
          </tr>
        </tbody>
      </table>
    </section>

    <section v-if="codesFor" class="panel">
      <h2>Коды сертификата «{{ codesFor }}»</h2>
      <p class="panel-sub">
        По одному коду в строке. Повторно загруженные коды пропускаются: файл от
        партнёра нередко присылают дважды.
      </p>
      <textarea v-model="codesText" class="codes" rows="6" placeholder="ABC-123&#10;DEF-456"></textarea>
      <div class="row">
        <button type="button" class="btn-primary" :disabled="!codesText || addingCodes" @click="addCodes">
          Загрузить
        </button>
        <button type="button" class="btn-link" @click="codesFor = ''">Закрыть</button>
      </div>
    </section>

    <section class="panel">
      <h2>Новый подарок или правка</h2>
      <div class="form-grid">
        <label class="field">
          <span>Код</span>
          <input v-model="draft.code" class="input mono" placeholder="tshirt_2026" />
        </label>
        <label class="field">
          <span>Род</span>
          <select v-model="draft.kind" class="input">
            <option value="BONUS">деньги на счёт</option>
            <option value="CERTIFICATE">сертификат из пула</option>
            <option value="PHYSICAL">вещь по купону</option>
            <option value="PROMO">общий промокод</option>
          </select>
        </label>
        <label class="field">
          <span>Название</span>
          <input v-model="draft.title" class="input" placeholder="Футболка платформы" />
        </label>
        <label class="field">
          <span>Описание</span>
          <input v-model="draft.description" class="input" />
        </label>
        <label class="field">
          <span>{{ draft.kind === 'BONUS' ? 'Сумма, ₽' : 'Номинал, ₽' }}</span>
          <input v-model.number="draft.amount" type="number" min="0" class="input" />
        </label>
        <label class="field">
          <span>Остаток на складе</span>
          <input v-model.number="draft.stock" type="number" min="0" class="input" placeholder="без ограничения" />
        </label>
        <label class="field">
          <span>Срок купона, дней</span>
          <input v-model.number="draft.valid_days" type="number" min="0" class="input" placeholder="бессрочно" />
        </label>
        <label v-if="draft.kind === 'PROMO'" class="field">
          <span>Промокод</span>
          <input v-model="draft.promo_code" class="input mono" />
        </label>
        <label class="field checkbox">
          <input v-model="draft.is_active" type="checkbox" />
          <span>активен</span>
        </label>
      </div>
      <button type="button" class="btn-primary" :disabled="!draft.code || savingGift" @click="saveGift">
        Сохранить подарок
      </button>
    </section>

    <section class="panel">
      <h2>Рассылка во внутреннюю почту</h2>
      <p class="panel-sub">
        Новость или акция уходит в ящик приложения — туда же, куда приходят
        выданные ачивки и купоны. Письма о выдачах пишет ядро; отсюда их послать
        нельзя.
      </p>
      <div class="form-grid">
        <label class="field">
          <span>Тип</span>
          <select v-model="mail.kind" class="input">
            <option value="NEWS">новость</option>
            <option value="PROMO">акция</option>
          </select>
        </label>
        <label class="field">
          <span>Кому</span>
          <select v-model="mail.role" class="input">
            <option value="">всем</option>
            <option value="EXECUTOR">исполнителям</option>
            <option value="CUSTOMER">заказчикам</option>
          </select>
        </label>
        <label class="field wide">
          <span>Тема</span>
          <input v-model="mail.subject" class="input" />
        </label>
      </div>
      <textarea v-model="mail.body" class="codes" rows="4" placeholder="Текст письма"></textarea>
      <button type="button" class="btn-primary" :disabled="!mail.subject || sending" @click="broadcast">
        Разослать
      </button>
    </section>
  </div>
</template>

<script lang="ts">
import { defineComponent, onMounted, reactive, ref } from 'vue'

import {
  adminAddGiftCodes,
  adminBroadcastMail,
  adminGetGifts,
  adminRedeemCoupon,
  adminSaveGift,
  type Gift,
} from '../../api/achievements'

const KIND_LABELS: Record<string, string> = {
  BONUS: 'деньги',
  CERTIFICATE: 'сертификат',
  PHYSICAL: 'вещь',
  PROMO: 'промокод',
}

export default defineComponent({
  name: 'AdminGifts',
  setup() {
    const gifts = ref<(Gift & { free_codes: number })[]>([])
    const loading = ref(false)
    const errorMsg = ref('')
    const successMsg = ref('')

    const coupon = ref('')
    const redeeming = ref(false)
    const redeemMsg = ref('')
    const redeemFailed = ref(false)

    const codesFor = ref('')
    const codesText = ref('')
    const addingCodes = ref(false)

    const savingGift = ref(false)
    const draft = reactive({
      code: '',
      kind: 'PHYSICAL' as Gift['kind'],
      title: '',
      description: '',
      amount: 0,
      stock: null as number | null,
      valid_days: null as number | null,
      promo_code: '',
      is_active: true,
    })

    const sending = ref(false)
    const mail = reactive({ kind: 'NEWS' as 'NEWS' | 'PROMO', role: '', subject: '', body: '' })

    const load = async () => {
      loading.value = true
      errorMsg.value = ''
      try {
        gifts.value = await adminGetGifts()
      } catch {
        errorMsg.value = 'Не удалось загрузить подарки.'
      } finally {
        loading.value = false
      }
    }

    const redeem = async () => {
      redeeming.value = true
      redeemMsg.value = ''
      try {
        const gift = await adminRedeemCoupon(coupon.value.trim().toUpperCase())
        redeemFailed.value = false
        redeemMsg.value = `Купон погашен: подарок «${gift.gift_code}».`
        coupon.value = ''
      } catch {
        redeemFailed.value = true
        redeemMsg.value = 'Купон недействителен, уже погашен или просрочен.'
      } finally {
        redeeming.value = false
      }
    }

    const openCodes = (code: string) => {
      codesFor.value = code
      codesText.value = ''
    }

    const addCodes = async () => {
      addingCodes.value = true
      try {
        const codes = codesText.value
          .split('\n')
          .map((line) => line.trim())
          .filter(Boolean)
        const added = await adminAddGiftCodes(codesFor.value, codes)
        successMsg.value = `Загружено кодов: ${added}.`
        codesText.value = ''
        await load()
      } catch {
        errorMsg.value = 'Не удалось загрузить коды.'
      } finally {
        addingCodes.value = false
      }
    }

    const saveGift = async () => {
      savingGift.value = true
      errorMsg.value = ''
      try {
        await adminSaveGift(draft.code, {
          kind: draft.kind,
          title: { ru: draft.title },
          description: { ru: draft.description },
          // Суммы на сервере в рублях: money.Amount разбирает их сам.
          amount: Number(draft.amount) || 0,
          stock: draft.stock ?? undefined,
          valid_days: draft.valid_days ?? undefined,
          is_active: draft.is_active,
          ...(draft.kind === 'PROMO' ? { promo_code: draft.promo_code } : {}),
        } as Partial<Gift>)
        successMsg.value = `Подарок «${draft.code}» сохранён.`
        await load()
      } catch (e) {
        const message = (e as { response?: { data?: string } })?.response?.data
        errorMsg.value = message || 'Не удалось сохранить подарок.'
      } finally {
        savingGift.value = false
      }
    }

    const broadcast = async () => {
      sending.value = true
      try {
        const sent = await adminBroadcastMail({
          kind: mail.kind,
          role: mail.role || undefined,
          subject: mail.subject,
          body: mail.body,
        })
        successMsg.value = `Разослано писем: ${sent}.`
        mail.subject = ''
        mail.body = ''
      } catch {
        errorMsg.value = 'Не удалось разослать.'
      } finally {
        sending.value = false
      }
    }

    const kindLabel = (kind: string) => KIND_LABELS[kind] ?? kind
    const formatAmount = (amount: number) => `${amount} ₽`
    const stockLabel = (gift: Gift & { free_codes: number }) => {
      if (gift.kind === 'CERTIFICATE') return `${gift.free_codes} код(ов)`
      if (gift.stock === undefined || gift.stock === null) return 'без ограничения'
      return String(gift.stock)
    }

    onMounted(load)

    return {
      gifts,
      loading,
      errorMsg,
      successMsg,
      coupon,
      redeeming,
      redeemMsg,
      redeemFailed,
      codesFor,
      codesText,
      addingCodes,
      savingGift,
      draft,
      sending,
      mail,
      load,
      redeem,
      openCodes,
      addCodes,
      saveGift,
      broadcast,
      kindLabel,
      formatAmount,
      stockLabel,
    }
  },
})
</script>

<style scoped>
.gifts-admin {
  padding: 20px;
  max-width: 1000px;
}

.page-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 20px;
}

h1 {
  font-size: 22px;
  margin: 0 0 6px;
}

h2 {
  font-size: 15px;
  margin: 0 0 4px;
}

.page-sub,
.panel-sub {
  color: #6b7280;
  font-size: 13px;
  margin: 0 0 12px;
  max-width: 640px;
}

.btn-refresh {
  border: none;
  background: #fff;
  border-radius: 10px;
  padding: 8px 12px;
  cursor: pointer;
}

.alert {
  padding: 10px 12px;
  border-radius: 10px;
  font-size: 13px;
  margin-bottom: 12px;
}

.alert.error {
  background: #fef2f2;
  color: #b91c1c;
}

.alert.success {
  background: #ecfdf5;
  color: #047857;
}

.panel {
  background: #fff;
  border-radius: 14px;
  padding: 16px;
  margin-bottom: 14px;
  border: 1px solid #eef0f4;
}

.row {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}

.input {
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  padding: 8px 10px;
  font-size: 13px;
}

.mono {
  font-family: ui-monospace, monospace;
  letter-spacing: 0.06em;
}

.btn-primary {
  border: none;
  background: #111827;
  color: #fff;
  border-radius: 10px;
  padding: 8px 16px;
  font-size: 13px;
  cursor: pointer;
}

.btn-primary:disabled {
  opacity: 0.5;
  cursor: default;
}

.btn-link {
  border: none;
  background: none;
  color: #2563eb;
  font-size: 13px;
  cursor: pointer;
}

.hint {
  font-size: 12px;
  color: #059669;
  margin: 8px 0 0;
}

.hint.error {
  color: #b91c1c;
}

.gift-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.gift-table th {
  text-align: left;
  color: #9ca3af;
  font-weight: 500;
  font-size: 12px;
  padding: 6px 8px;
}

.gift-table td {
  padding: 8px;
  border-top: 1px solid #f3f4f6;
}

.badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 999px;
}

.badge.ok {
  background: #ecfdf5;
  color: #059669;
}

.badge.muted {
  background: #f3f4f6;
  color: #9ca3af;
}

.empty {
  color: #9ca3af;
  text-align: center;
  padding: 16px;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
  margin-bottom: 12px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  color: #4b5563;
}

.field.wide {
  grid-column: 1 / -1;
}

.field.checkbox {
  flex-direction: row;
  align-items: center;
  gap: 8px;
}

.codes {
  width: 100%;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  padding: 10px;
  font-size: 13px;
  font-family: ui-monospace, monospace;
  margin-bottom: 10px;
}
</style>
