<template>
  <div class="passport-panel">
    <p v-if="error" class="alert error">{{ error }}</p>
    <p v-if="notice" class="alert success">{{ notice }}</p>

    <div v-if="status" class="pp-status">
      <span class="pp-badge" :class="status.is_checked ? 'ok' : ''">
        {{ status.is_checked ? $t('passport.admin.checked') : $t('passport.admin.notChecked') }}
      </span>
      <span class="muted">
        <template v-if="status.exists">
          {{ $t('passport.admin.exists') }} · {{ status.has_photo ? $t('passport.card.photoYes') : $t('passport.card.photoNo') }}
          · {{ $t('passport.admin.source.' + (status.source || 'OWNER')) }}
        </template>
        <template v-else>{{ $t('passport.admin.none') }}</template>
      </span>
      <!-- Откуда снимок: подпись приложения. Это подсказка модератору, а не
           запрет — снимок без подписи повод посмотреть внимательнее. -->
      <span v-if="status.has_photo" class="pp-origin" :class="photoOrigin" :title="$t('passport.admin.photoOriginHint')">
        {{ $t('passport.admin.photoOrigin.' + photoOrigin) }}
      </span>
      <span v-if="!status.is_checked && status.check_requested_at" class="pp-badge wait">
        {{ $t('passport.admin.checkRequested') }}
      </span>
      <span v-if="!status.consent_given" class="pp-warn">{{ $t('passport.admin.noConsent') }}</span>
    </div>

    <!-- «Проверенный»: поставить можно только при паспорте с фото; причина —
         в обе стороны, она уходит в аудит. -->
    <div v-if="can('checks.edit') && status" class="pp-check">
      <input v-model="reason" class="input" :placeholder="$t('passport.admin.reason')" />
      <button
        v-if="!status.is_checked"
        type="button"
        class="btn-more"
        :disabled="busy || !reason.trim() || !status.has_photo"
        @click="setChecked(true)"
      >
        {{ $t('passport.admin.markChecked') }}
      </button>
      <button v-else type="button" class="btn-link danger" :disabled="busy || !reason.trim()" @click="setChecked(false)">
        {{ $t('passport.admin.unmarkChecked') }}
      </button>
    </div>

    <!-- Сам паспорт — по кнопке: каждый показ пишется в журнал. -->
    <template v-if="can('passports.view') && status?.exists">
      <button v-if="!full" type="button" class="btn-more" :disabled="busy" @click="reveal">
        <i class="ph-bold ph-eye"></i> {{ $t('passport.admin.reveal') }}
      </button>
      <p v-if="!full" class="muted small">{{ $t('passport.admin.revealHint') }}</p>
      <div v-if="full && !editing" class="pp-full">
        <div><span>{{ $t('passport.fields.seriesNumber') }}</span><strong>{{ full.series }} {{ full.number }}</strong></div>
        <div><span>{{ $t('passport.fields.issuedAt') }}</span><strong>{{ full.issued_at }}</strong></div>
        <button v-if="full.has_photo && !photoUrl" type="button" class="btn-link" :disabled="busy" @click="showPhoto">
          {{ $t('passport.admin.showPhoto') }}
        </button>
        <img v-if="photoUrl" :src="photoUrl" class="pp-photo" alt="" />
      </div>
    </template>

    <!-- Кто обращался к этому паспорту. Открывается по кнопке: журнал нужен,
         когда его спрашивают, а не на каждом открытии карточки. -->
    <template v-if="can('passports.view')">
      <button v-if="!accessOpen" type="button" class="btn-link" :disabled="busy" @click="openAccess">
        <i class="ph-bold ph-eyes"></i> {{ $t('passport.admin.whoLooked') }}
      </button>
      <div v-else class="pp-access">
        <div class="pp-access-head">
          <strong>{{ $t('passport.admin.whoLooked') }}</strong>
          <button type="button" class="btn-link" @click="accessOpen = false">{{ $t('common.close') }}</button>
        </div>
        <p v-if="access.length === 0" class="muted small">{{ $t('passport.admin.accessEmpty') }}</p>
        <div v-for="row in access" :key="row.id" class="pp-access-row">
          <span>{{ formatDate(row.created_at) }}</span>
          <span>{{ $t('passport.admin.action.' + row.action) }}</span>
          <span class="muted">{{ row.self ? $t('passport.admin.accessSelf') : (row.viewer_name || row.viewer_phone) }}</span>
        </div>
        <router-link v-if="can('document_audit.view')" class="btn-link" :to="{ path: '/admin/document-audit', query: { user_id: userId } }">
          {{ $t('passport.admin.wholeAudit') }}
        </router-link>
      </div>
    </template>

    <!-- Правка: внести, исправить, заменить фото, удалить. -->
    <template v-if="can('passports.edit') && status">
      <div v-if="editing" class="pp-edit">
        <PassportFields v-model="draft" :errors="fieldErrors" />
        <div class="pp-actions">
          <button type="button" class="btn-more" :disabled="busy" @click="save">{{ $t('common.save') }}</button>
          <button type="button" class="btn-link" :disabled="busy" @click="editing = false">{{ $t('common.cancel') }}</button>
        </div>
      </div>
      <div v-else class="pp-actions">
        <button type="button" class="btn-link" :disabled="busy || !status.consent_given" @click="startEdit">
          {{ status.exists ? $t('passport.admin.edit') : $t('passport.admin.add') }}
        </button>
        <label v-if="status.exists" class="btn-link pp-file">
          {{ status.has_photo ? $t('passport.card.replacePhoto') : $t('passport.card.addPhoto') }}
          <input type="file" accept="image/jpeg,image/png,image/webp" :disabled="busy" @change="uploadPhoto" />
        </label>
        <button v-if="status.exists" type="button" class="btn-link danger" :disabled="busy" @click="remove">
          {{ $t('passport.admin.delete') }}
        </button>
      </div>
    </template>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import PassportFields from '../../components/passport/PassportFields.vue'
import {
  adminDeletePassport,
  adminGetPassport,
  adminGetPassportPhoto,
  adminPassportStatus,
  adminSavePassport,
  adminSetChecked,
  adminUploadPassportPhoto,
  emptyPassport,
  passportError,
  passportErrorText,
  userPassportAccess,
  type PassportAccess,
  type PassportData,
  type PassportFull,
  type PassportStatus,
} from '../../api/passport'

// Паспорт и «проверенный» в карточке пользователя
// (implementation_plan_delivery_passport.md §2, §3.1). Права раздельные:
// checks — отметка, passports.view — показ (с журналом), passports.edit — правка.
export default defineComponent({
  name: 'PassportAdminPanel',
  components: { PassportFields },
  props: {
    userId: { type: String, required: true },
  },
  setup(props) {
    const { t } = useI18n()
    const authStore = useAuthStore()
    const can = (permission: string) => authStore.can(permission)

    const status = ref<PassportStatus | null>(null)
    const full = ref<PassportFull | null>(null)
    const photoUrl = ref('')
    const draft = ref<PassportData>(emptyPassport())
    const fieldErrors = ref<Record<string, string>>({})
    const editing = ref(false)
    const accessOpen = ref(false)
    const access = ref<PassportAccess[]>([])
    const reason = ref('')
    const busy = ref(false)
    const error = ref('')
    const notice = ref('')

    const dropPhoto = () => {
      if (photoUrl.value) URL.revokeObjectURL(photoUrl.value)
      photoUrl.value = ''
    }

    const run = async (fn: () => Promise<string | void>) => {
      busy.value = true
      error.value = ''
      notice.value = ''
      try {
        const text = await fn()
        if (text) notice.value = text
      } catch (err) {
        fieldErrors.value = passportError(err)?.fields || {}
        error.value = passportErrorText(err, t('passport.admin.failed'))
      } finally {
        busy.value = false
      }
    }

    const loadStatus = () =>
      run(async () => {
        status.value = await adminPassportStatus(props.userId)
      })

    const reset = () => {
      full.value = null
      dropPhoto()
      editing.value = false
      accessOpen.value = false
      access.value = []
      reason.value = ''
      loadStatus()
    }

    const reveal = () =>
      run(async () => {
        full.value = await adminGetPassport(props.userId)
      })

    const showPhoto = () =>
      run(async () => {
        dropPhoto()
        photoUrl.value = URL.createObjectURL(await adminGetPassportPhoto(props.userId))
      })

    const startEdit = () => {
      draft.value = full.value
        ? { series: full.value.series, number: full.value.number, issued_at: full.value.issued_at }
        : emptyPassport()
      fieldErrors.value = {}
      editing.value = true
    }

    const save = () =>
      run(async () => {
        full.value = await adminSavePassport(props.userId, draft.value)
        editing.value = false
        status.value = await adminPassportStatus(props.userId)
        return t('passport.admin.saved')
      })

    const uploadPhoto = (event: Event) => {
      const input = event.target as HTMLInputElement
      const file = input.files?.[0]
      input.value = ''
      if (!file) return
      run(async () => {
        await adminUploadPassportPhoto(props.userId, file)
        dropPhoto()
        status.value = await adminPassportStatus(props.userId)
        return t('passport.card.photoSaved')
      })
    }

    const remove = () => {
      if (!window.confirm(t('passport.admin.deleteConfirm'))) return
      run(async () => {
        await adminDeletePassport(props.userId)
        full.value = null
        dropPhoto()
        status.value = await adminPassportStatus(props.userId)
        return t('passport.admin.deleted')
      })
    }

    // Подпись цела — снимок такой, каким его сделало приложение; метка без
    // подписи — снимок из приложения, но файл пересохранён; ни того ни другого
    // — фото принесли готовым.
    const photoOrigin = computed(() => {
      if (status.value?.photo_seal === 'VALID') return 'app'
      if (status.value?.photo_mark === 'FOUND') return 'resaved'
      return 'foreign'
    })

    const openAccess = () =>
      run(async () => {
        access.value = (await userPassportAccess(props.userId, { limit: 20 })).items
        accessOpen.value = true
      })

    const formatDate = (value: string) => new Date(value).toLocaleString('ru-RU')

    const setChecked = (checked: boolean) =>
      run(async () => {
        await adminSetChecked(props.userId, checked, reason.value.trim())
        reason.value = ''
        status.value = await adminPassportStatus(props.userId)
        return checked ? t('passport.admin.markedChecked') : t('passport.admin.unmarked')
      })

    onMounted(loadStatus)
    watch(() => props.userId, reset)
    onBeforeUnmount(dropPhoto)

    return {
      can, status, full, photoUrl, draft, fieldErrors, editing, reason, busy, error, notice, photoOrigin,
      accessOpen, access, openAccess, formatDate,
      reveal, showPhoto, startEdit, save, uploadPhoto, remove, setChecked,
    }
  },
})
</script>

<style scoped>
.passport-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 4px 0;
}
.alert {
  padding: 8px 12px;
  border-radius: 10px;
  font-size: 13px;
  margin: 0;
}
.alert.error {
  background: #fef2f2;
  color: #b91c1c;
}
.alert.success {
  background: #ecfdf5;
  color: #047857;
}
.passport-panel > .btn-more {
  align-self: flex-start;
}
.pp-status {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
  font-size: 13px;
}
.pp-badge {
  border-radius: 999px;
  padding: 3px 10px;
  background: #f1f5f9;
  color: #475569;
  font-weight: 600;
}
.pp-badge.ok {
  background: #dcfce7;
  color: #15803d;
}
.pp-badge.wait {
  background: #fef3c7;
  color: #b45309;
}
.pp-warn {
  color: #b45309;
}
.pp-origin {
  border-radius: 999px;
  padding: 3px 10px;
  background: #f1f5f9;
  color: #475569;
  cursor: help;
}
.pp-origin.app {
  background: #dcfce7;
  color: #15803d;
}
.pp-origin.resaved {
  background: #fef3c7;
  color: #b45309;
}
.pp-origin.foreign {
  background: #fee2e2;
  color: #b91c1c;
}
.pp-check,
.pp-actions {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
.pp-check .input {
  flex: 1;
  min-width: 200px;
}
.input {
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  padding: 8px 10px;
  font-size: 13px;
}
.pp-full {
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 13px;
}
.pp-access {
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
}
.pp-access-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
}
.pp-access-row {
  display: grid;
  grid-template-columns: 150px 130px 1fr;
  gap: 8px;
}
@media (max-width: 640px) {
  .pp-access-row {
    grid-template-columns: 1fr;
    gap: 2px;
    padding-bottom: 4px;
    border-bottom: 1px solid #f1f5f9;
  }
}
.pp-full > div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}
.pp-photo {
  max-width: 100%;
  border-radius: 8px;
  margin-top: 6px;
}
.pp-file input {
  display: none;
}
.btn-more,
.btn-link {
  border: none;
  cursor: pointer;
  font-size: 13px;
}
.btn-more {
  background: #0f766e;
  color: #fff;
  border-radius: 10px;
  padding: 8px 12px;
  display: inline-flex;
  gap: 6px;
  align-items: center;
}
.btn-link {
  background: none;
  color: #0f766e;
  padding: 4px 0;
}
.btn-link.danger {
  color: #b91c1c;
}
.muted {
  color: #64748b;
}
.small {
  font-size: 12px;
  margin: 0;
}
</style>
