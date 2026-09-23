<template>
  <div class="passport-card">
    <div class="pc-head">
      <div class="pc-title">
        <i class="ph-fill ph-identification-card"></i>
        {{ $t('passport.card.title') }}
        <!-- Слово-статус нажимается: что такое «проверенный» и зачем он, надо
             объяснить там же, где человек его видит. -->
        <button
          type="button"
          class="pc-badge"
          :class="checked ? 'ok' : 'plain'"
          :aria-expanded="showAbout"
          @click="showAbout = !showAbout"
        >
          <i class="ph-bold" :class="checked ? 'ph-seal-check' : 'ph-question'"></i>
          {{ checked ? $t('passport.card.checked') : $t('passport.card.checkedWord') }}
        </button>
      </div>
      <div class="pc-sub">{{ $t('passport.card.sub') }}</div>
      <div v-if="showAbout" class="pc-about">
        <strong>{{ $t('passport.card.aboutTitle') }}</strong>
        <p>{{ $t('passport.card.about') }}</p>
        <button type="button" class="pc-btn secondary" @click="showAbout = false">{{ $t('common.close') }}</button>
      </div>
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
          <button type="button" class="pc-btn secondary" @click="openSupport">{{ $t('passport.card.editViaSupport') }}</button>
        </template>
        <template v-else>
          <template v-if="editing">
            <button type="button" class="pc-btn" :disabled="busy" @click="save">{{ $t('common.save') }}</button>
            <button v-if="mask?.exists" type="button" class="pc-btn secondary" :disabled="busy" @click="editing = false">{{ $t('common.cancel') }}</button>
          </template>
          <button v-else type="button" class="pc-btn secondary" @click="startEdit">{{ $t('passport.card.edit') }}</button>
          <button v-if="mask?.exists && !editing" type="button" class="pc-btn secondary" :disabled="busy" @click="takePhoto">
            <i class="ph-bold ph-camera"></i>
            {{ mask.has_photo ? $t('passport.card.replacePhoto') : $t('passport.card.addPhoto') }}
          </button>
        </template>
      </div>
      <!-- Заявка на статус уходит сама, как только паспорт полон: кнопки «стать
           проверенным» нет и писать в поддержку не нужно. -->
      <div v-if="mask?.exists && !mask.locked" class="pc-muted">{{ $t('passport.card.photoLive') }}</div>
      <div v-if="!checked && mask?.check_requested_at" class="pc-note">{{ $t('passport.card.checkPending') }}</div>
      <div v-else-if="!checked && !(mask?.exists && mask.has_photo)" class="pc-muted">{{ $t('passport.card.checkedHint') }}</div>
    </template>

    <!-- Запасной путь для браузера: своей камеры у него нет. -->
    <input
      ref="fileInput"
      type="file"
      accept="image/jpeg"
      capture="environment"
      style="display: none"
      @change="onFileChosen"
    />

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
  getMyPassportData,
  myPassportPhotoKey,
  passportError,
  passportErrorText,
  saveMyPassport,
  uploadMyPassportPhoto,
  type PassportData,
  type PassportMask,
} from '../../api/passport'
import { cameraAvailable, shootPassport, shotAt, signPassportPhoto, toRfc3339 } from './photo'
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
    const showAbout = ref(false)
    const fileInput = ref<HTMLInputElement | null>(null)
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

    // Правка начинается с того, что уже сохранено: набранное однажды не
    // набирают снова. Маска — для показа, за данными идём отдельным запросом.
    const startEdit = async () => {
      errors.value = {}
      say('')
      draft.value = emptyPassport()
      editing.value = true
      if (!mask.value?.exists) return
      busy.value = true
      try {
        draft.value = await getMyPassportData()
      } catch (err) {
        say(passportErrorText(err, t('passport.card.loadFailed')), true)
      } finally {
        busy.value = false
      }
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

    // Снимок подписывается ключом, который сервер выдаёт перед съёмкой: так
    // видно, что фото сделано в приложении, а не принесено готовым. Подпись не
    // удалась — фото всё равно уходит: снимок нужнее, чем отметка о нём.
    const sendPhoto = async (original: Uint8Array, takenAt: Date) => {
      busy.value = true
      try {
        let photo = original
        let at: string | undefined
        try {
          photo = await signPassportPhoto(original, await myPassportPhotoKey(), takenAt)
          at = toRfc3339(takenAt)
        } catch (err) {
          console.warn('[passport] cannot sign the photo', err)
        }
        mask.value = await uploadMyPassportPhoto(new Blob([photo as BlobPart], { type: 'image/jpeg' }), at)
        say(t('passport.card.photoSaved'))
      } catch (err) {
        say(passportErrorText(err, t('passport.card.saveFailed')), true)
      } finally {
        busy.value = false
      }
    }

    // Фото — только живой съёмкой. В приложении открывается камера; браузеру
    // своей камеры не дать, там остаётся системный выбор.
    const takePhoto = async () => {
      say('')
      if (!cameraAvailable()) {
        fileInput.value?.click()
        return
      }
      try {
        const original = await shootPassport()
        if (original) await sendPhoto(original, shotAt())
      } catch (err) {
        // Съёмку закрыли — это не ошибка.
        console.warn('[passport] camera closed', err)
      }
    }

    const onFileChosen = async (event: Event) => {
      const input = event.target as HTMLInputElement
      const file = input.files?.[0]
      input.value = ''
      if (!file) return
      await sendPhoto(new Uint8Array(await file.arrayBuffer()), shotAt())
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

    // В поддержку идут только за правкой проверенного паспорта: заявку на сам
    // статус ставит сервер, когда паспорт становится полным.
    const openSupport = () => {
      supportPrefill.value = t('passport.card.editRequest')
      showSupport.value = true
    }

    onMounted(load)

    return {
      mask, draft, errors, editing, loading, busy, message, messageIsError, showSupport, supportPrefill, fileInput, showAbout,
      checked, consentRequired, startEdit, save, takePhoto, onFileChosen, accept, openSupport,
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
.pc-badge {
  border: none;
  font-family: inherit;
  cursor: pointer;
}
.pc-badge.ok {
  background: #dcfce7;
  color: #15803d;
}
.pc-badge.plain {
  background: #f1f5f9;
  color: #475569;
}
.pc-about {
  margin-top: 10px;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 12px;
  font-size: 13px;
  color: #334155;
  line-height: 1.45;
}
.pc-about p {
  margin: 6px 0 8px;
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
</style>
