<template>
  <div class="passport-card">
    <div class="pc-head">
      <div class="pc-title">
        <i class="ph-fill ph-identification-card"></i>
        {{ $t('passport.card.title') }}
        <span v-if="checked" class="pc-badge ok"><i class="ph-bold ph-seal-check"></i> {{ $t('passport.card.checked') }}</span>
      </div>
      <div class="pc-sub">{{ $t('passport.card.sub') }}</div>
    </div>

    <div v-if="loading" class="pc-muted">{{ $t('common.loading') }}</div>

    <!-- Без согласия на обработку персональных данных паспорт не хранится. -->
    <div v-else-if="consentRequired" class="pc-note">
      {{ $t('passport.card.consentFirst') }}
      <button type="button" class="pc-btn" :disabled="busy" @click="accept">{{ $t('passport.consent.accept') }}</button>
    </div>

    <template v-else>
      <div v-if="mask?.exists && !editing" class="pc-mask">
        <div class="pc-row"><span>{{ $t('passport.fields.seriesNumber') }}</span><strong>{{ mask.series }} {{ mask.number }}</strong></div>
        <div class="pc-row"><span>{{ $t('passport.fields.issuedYear') }}</span><strong>{{ mask.issued_year }}</strong></div>
        <div class="pc-row">
          <span>{{ $t('passport.fields.photo') }}</span>
          <strong :class="mask.has_photo ? 'ok' : 'warn'">{{ mask.has_photo ? $t('passport.card.photoYes') : $t('passport.card.photoNo') }}</strong>
        </div>
      </div>

      <PassportFields v-if="editing" v-model="draft" :errors="errors" />

      <p v-if="message" class="pc-msg" :class="{ error: messageIsError }">{{ message }}</p>

      <div class="pc-actions">
        <!-- Проверенный паспорт правится через поддержку: иначе его можно было бы подменить. -->
        <template v-if="mask?.locked">
          <button type="button" class="pc-btn secondary" @click="openSupport('edit')">{{ $t('passport.card.editViaSupport') }}</button>
        </template>
        <template v-else>
          <template v-if="editing">
            <button type="button" class="pc-btn" :disabled="busy" @click="save">{{ $t('common.save') }}</button>
            <button v-if="mask?.exists" type="button" class="pc-btn secondary" :disabled="busy" @click="editing = false">{{ $t('common.cancel') }}</button>
          </template>
          <button v-else type="button" class="pc-btn secondary" @click="startEdit">{{ $t('passport.card.edit') }}</button>
          <label v-if="mask?.exists && !editing" class="pc-btn secondary pc-file">
            <i class="ph-bold ph-camera"></i>
            {{ mask.has_photo ? $t('passport.card.replacePhoto') : $t('passport.card.addPhoto') }}
            <input type="file" accept="image/jpeg,image/png,image/webp" capture="environment" :disabled="busy" @change="uploadPhoto" />
          </label>
        </template>
        <button
          v-if="!checked && mask?.exists && mask.has_photo && !mask.check_requested_at"
          type="button"
          class="pc-btn accent"
          @click="openSupport('check')"
        >
          {{ $t('passport.card.becomeChecked') }}
        </button>
      </div>
      <!-- Заявка уже есть (паспорт отдан на верификации) — просить нечего. -->
      <div v-if="!checked && mask?.check_requested_at" class="pc-note">{{ $t('passport.card.checkPending') }}</div>
      <div v-else-if="!checked && !(mask?.exists && mask.has_photo)" class="pc-muted">{{ $t('passport.card.checkedHint') }}</div>
    </template>

    <SupportChatModal v-model:show="showSupport" :prefill="supportPrefill" />
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import {
  acceptPDConsent,
  emptyPassport,
  getMyPassport,
  passportError,
  passportErrorText,
  saveMyPassport,
  uploadMyPassportPhoto,
  type PassportData,
  type PassportMask,
} from '../../api/passport'
import PassportFields from './PassportFields.vue'
import SupportChatModal from '../SupportChatModal.vue'

// Паспорт в профиле (implementation_plan_delivery_passport.md §2, §3.2):
// маска, правка, фото и путь к статусу «проверенный» — обращение в поддержку.
export default defineComponent({
  name: 'PassportCard',
  components: { PassportFields, SupportChatModal },
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()
    const mask = ref<PassportMask | null>(null)
    const draft = ref<PassportData>(emptyPassport())
    const errors = ref<Record<string, string>>({})
    const editing = ref(false)
    const loading = ref(true)
    const busy = ref(false)
    const message = ref('')
    const messageIsError = ref(false)
    const showSupport = ref(false)
    const supportPrefill = ref('')

    const checked = computed(() => !!authStore.user?.is_checked)
    const consentRequired = computed(() => !!authStore.user?.pd_consent_required)

    const say = (text: string, isError = false) => {
      message.value = text
      messageIsError.value = isError
    }

    const load = async () => {
      loading.value = true
      try {
        mask.value = await getMyPassport()
        editing.value = !mask.value.exists && !mask.value.locked
      } catch (err) {
        say(passportErrorText(err, t('passport.card.loadFailed')), true)
      } finally {
        loading.value = false
      }
    }

    // Полные данные наружу не отдаются никому, кроме права passports.view, —
    // и владельцу тоже: правка начинается с пустой формы.
    const startEdit = () => {
      draft.value = emptyPassport()
      errors.value = {}
      editing.value = true
      say('')
    }

    const save = async () => {
      busy.value = true
      errors.value = {}
      try {
        mask.value = await saveMyPassport(draft.value)
        editing.value = false
        say(mask.value.has_photo ? t('passport.card.saved') : t('passport.card.savedAddPhoto'))
      } catch (err) {
        errors.value = passportError(err)?.fields || {}
        say(passportErrorText(err, t('passport.card.saveFailed')), true)
      } finally {
        busy.value = false
      }
    }

    const uploadPhoto = async (event: Event) => {
      const input = event.target as HTMLInputElement
      const file = input.files?.[0]
      input.value = ''
      if (!file) return
      busy.value = true
      try {
        mask.value = await uploadMyPassportPhoto(file)
        say(t('passport.card.photoSaved'))
      } catch (err) {
        say(passportErrorText(err, t('passport.card.saveFailed')), true)
      } finally {
        busy.value = false
      }
    }

    const accept = async () => {
      busy.value = true
      try {
        await acceptPDConsent()
        await authStore.fetchMe()
        await load()
      } catch {
        say(t('passport.consent.failed'), true)
      } finally {
        busy.value = false
      }
    }

    // Статус ставит модератор по обращению — тем же приёмом, что возврат
    // покупки: чат поддержки с подставленным текстом, отправляет сам человек.
    const openSupport = (reason: 'check' | 'edit') => {
      supportPrefill.value = reason === 'check' ? t('passport.card.checkRequest') : t('passport.card.editRequest')
      showSupport.value = true
    }

    onMounted(load)

    return {
      mask, draft, errors, editing, loading, busy, message, messageIsError, showSupport, supportPrefill,
      checked, consentRequired, startEdit, save, uploadPhoto, accept, openSupport,
    }
  },
})
</script>

<style scoped>
.passport-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 20px;
}
.pc-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 700;
  font-size: 15px;
  color: #0f172a;
  flex-wrap: wrap;
}
.pc-title .ph-identification-card {
  color: #0ea5e9;
}
.pc-sub,
.pc-muted {
  color: #64748b;
  font-size: 13px;
}
.pc-badge {
  font-size: 12px;
  border-radius: 999px;
  padding: 2px 8px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.pc-badge.ok {
  background: #dcfce7;
  color: #15803d;
}
.pc-mask {
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.pc-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  font-size: 14px;
  color: #475569;
}
.pc-row .ok {
  color: #15803d;
}
.pc-row .warn {
  color: #b45309;
}
.pc-note {
  background: #fffbeb;
  color: #92400e;
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 13px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-start;
}
.pc-msg {
  margin: 0;
  font-size: 13px;
  color: #15803d;
}
.pc-msg.error {
  color: #dc2626;
}
.pc-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.pc-btn {
  border: none;
  border-radius: 12px;
  padding: 9px 14px;
  font-weight: 600;
  font-size: 13px;
  cursor: pointer;
  background: #0f766e;
  color: #fff;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.pc-btn.secondary {
  background: #f1f5f9;
  color: #334155;
}
.pc-btn.accent {
  background: #10b981;
}
.pc-btn:disabled {
  opacity: 0.6;
  cursor: default;
}
.pc-file input {
  display: none;
}
</style>
