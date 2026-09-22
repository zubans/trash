<template>
  <div class="shop-admin">
    <header class="page-head">
      <p class="page-sub">
        Где забирают купленные вещи. Выключенный пункт не предлагается при оформлении,
        но остаётся в справочнике: на него ссылаются прошлые покупки.
      </p>
      <div class="toolbar">
        <router-link to="/admin/shop/products" class="btn-secondary">{{ $t('shop.admin.products') }}</router-link>
        <button v-if="can('shop.edit')" type="button" class="btn-primary" @click="startNew">
          <i class="ph-bold ph-plus"></i> {{ $t('shop.admin.newPoint') }}
        </button>
      </div>
    </header>

    <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>

    <section class="panel">
      <table class="table">
        <tbody>
          <tr v-for="p in points" :key="p.id" class="clickable" :class="{ selected: draftId === p.id }" @click="edit(p)">
            <td>{{ p.title?.ru }}</td>
            <td>{{ p.address }}</td>
            <td class="muted">{{ p.hours }}</td>
            <td>
              <span class="badge" :class="p.is_active ? 'ok' : ''">
                {{ p.is_active ? $t('shop.admin.active') : $t('shop.admin.inactive') }}
              </span>
            </td>
          </tr>
          <tr v-if="!points.length">
            <td colspan="4" class="empty">{{ $t('shop.admin.pointsEmpty') }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <section v-if="draft" class="panel">
      <div class="form-grid">
        <label class="field">
          <span>{{ $t('shop.admin.fields.titleRu') }}</span>
          <input v-model="draft.titleRu" class="input" :class="{ invalid: fieldErrors.title }" />
          <span v-if="fieldErrors.title" class="field-error">{{ fieldErrors.title }}</span>
        </label>
        <label class="field">
          <span>{{ $t('shop.admin.fields.titleEn') }}</span>
          <input v-model="draft.titleEn" class="input" />
        </label>
        <label class="field wide">
          <span>{{ $t('shop.admin.address') }}</span>
          <input v-model="draft.address" class="input" :class="{ invalid: fieldErrors.address }" />
          <span v-if="fieldErrors.address" class="field-error">{{ fieldErrors.address }}</span>
        </label>
        <label class="field">
          <span>{{ $t('shop.admin.hours') }}</span>
          <input v-model="draft.hours" class="input" placeholder="Пн–Пт 10:00–19:00" />
        </label>
        <label class="field checkbox">
          <input v-model="draft.isActive" type="checkbox" /> {{ $t('shop.admin.pointActive') }}
        </label>
      </div>
      <div class="row">
        <button type="button" class="btn-primary" :disabled="saving" @click="save">{{ $t('common.save') }}</button>
        <button type="button" class="btn-secondary" @click="draft = null">{{ $t('common.cancel') }}</button>
      </div>
    </section>
  </div>
</template>

<script lang="ts">
import { defineComponent, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import { adminGetPickupPoints, adminSavePickupPoint, shopError, shopErrorText, type PickupPoint } from '../../api/shop'

interface Draft {
  titleRu: string
  titleEn: string
  address: string
  hours: string
  isActive: boolean
}

export default defineComponent({
  name: 'ShopPickupPoints',
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()
    const can = (permission: string) => authStore.can(permission)

    const points = ref<PickupPoint[]>([])
    const draft = ref<Draft | null>(null)
    const draftId = ref<string | null>(null)
    const saving = ref(false)
    const errorMsg = ref('')
    const fieldErrors = ref<Record<string, string>>({})

    const load = async () => {
      try {
        points.value = await adminGetPickupPoints()
      } catch (err) {
        errorMsg.value = shopErrorText(err, t, t('shop.loadFailed'))
      }
    }

    const startNew = () => {
      draftId.value = null
      fieldErrors.value = {}
      draft.value = { titleRu: '', titleEn: '', address: '', hours: '', isActive: true }
    }

    const edit = (p: PickupPoint) => {
      if (!can('shop.edit')) return
      draftId.value = p.id
      fieldErrors.value = {}
      draft.value = { titleRu: p.title?.ru || '', titleEn: p.title?.en || '', address: p.address, hours: p.hours || '', isActive: p.is_active }
    }

    const save = async () => {
      if (!draft.value) return
      saving.value = true
      errorMsg.value = ''
      fieldErrors.value = {}
      try {
        const d = draft.value
        await adminSavePickupPoint(draftId.value, {
          title: { ru: d.titleRu.trim(), ...(d.titleEn.trim() ? { en: d.titleEn.trim() } : {}) },
          address: d.address.trim(),
          hours: d.hours.trim() || undefined,
          is_active: d.isActive,
        })
        draft.value = null
        await load()
      } catch (err) {
        fieldErrors.value = shopError(err)?.fields || {}
        errorMsg.value = shopErrorText(err, t, t('shop.errors.generic'))
      } finally {
        saving.value = false
      }
    }

    onMounted(load)

    return { can, points, draft, draftId, saving, errorMsg, fieldErrors, startNew, edit, save }
  },
})
</script>

<style scoped src="./shop-admin.css"></style>
