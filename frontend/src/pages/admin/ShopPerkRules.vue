<template>
  <div class="shop-admin">
    <header class="page-head">
      <p class="page-sub">{{ $t('shop.admin.rules.sub') }}</p>
      <div class="toolbar">
        <router-link to="/admin/shop/products" class="btn-secondary">{{ $t('shop.admin.products') }}</router-link>
        <button v-if="can('perk_rules.create')" type="button" class="btn-primary" @click="startNew">
          <i class="ph-bold ph-plus"></i> {{ $t('shop.admin.rules.new') }}
        </button>
      </div>
    </header>

    <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>
    <p v-if="successMsg" class="alert success">{{ successMsg }}</p>

    <section class="panel">
      <table class="table">
        <tbody>
          <tr v-for="r in rules" :key="r.code" class="clickable" :class="{ selected: draft?.code === r.code }" @click="edit(r)">
            <td>
              {{ r.title }}
              <div class="muted mono">{{ r.code }}</div>
            </td>
            <td class="muted">{{ r.description }}</td>
            <td>
              <span class="badge">{{ r.origin === 'SHIPPED' ? $t('shop.admin.rules.shipped') : $t('shop.admin.rules.own') }}</span>
            </td>
            <td>
              <span class="badge" :class="r.is_active ? 'ok' : ''">
                {{ r.is_active ? $t('shop.admin.active') : $t('shop.admin.inactive') }}
              </span>
            </td>
          </tr>
          <tr v-if="!rules.length">
            <td colspan="4" class="empty">—</td>
          </tr>
        </tbody>
      </table>
    </section>

    <section v-if="draft" class="panel">
      <!-- Поставляемое правило приезжает со сборкой: его можно только включить
           или выключить. Его текст — образец для собственного. -->
      <p v-if="draft.origin === 'SHIPPED'" class="panel-sub">{{ $t('shop.admin.rules.shippedHint') }}</p>
      <div class="form-grid">
        <label class="field">
          <span>{{ $t('shop.admin.rules.code') }}</span>
          <input v-model="draft.code" class="input mono" :disabled="!isNew" />
        </label>
        <label class="field">
          <span>{{ $t('shop.admin.rules.title') }}</span>
          <input v-model="draft.title" class="input" :disabled="draft.origin === 'SHIPPED'" />
        </label>
        <label class="field wide">
          <span>{{ $t('shop.admin.rules.source') }}</span>
          <textarea
            v-model="draft.source"
            class="input mono source"
            rows="12"
            spellcheck="false"
            :disabled="draft.origin === 'SHIPPED'"
            :class="{ invalid: fieldErrors.source }"
          ></textarea>
          <span v-if="fieldErrors.source" class="field-error">{{ fieldErrors.source }}</span>
        </label>
        <label class="field checkbox">
          <input v-model="draft.isActive" type="checkbox" /> {{ $t('shop.admin.rules.active') }}
        </label>
      </div>

      <div class="toolbar">
        <button v-if="draft.origin === 'SHIPPED'" type="button" class="btn-secondary" @click="copyAsNew">
          {{ $t('shop.admin.rules.copy') }}
        </button>
        <button v-if="draft.origin !== 'SHIPPED'" type="button" class="btn-secondary" :disabled="checking" @click="check">
          {{ $t('shop.admin.rules.check') }}
        </button>
        <button v-if="canSave" type="button" class="btn-primary" :disabled="saving" @click="save">{{ $t('common.save') }}</button>
        <button type="button" class="btn-link" @click="draft = null">{{ $t('shop.admin.cancel') }}</button>
      </div>

      <!-- Прогон по сетке: какую ставку правило даёт при каждой базе и уровне.
           Правило, у которого хоть один ответ вне [0, база], не сохраняется. -->
      <div v-if="grid.length" class="table-wrap grid">
        <h3>{{ $t('shop.admin.rules.grid') }}</h3>
        <table class="table">
          <thead>
            <tr>
              <th>{{ $t('shop.admin.rules.base') }}</th>
              <th>{{ $t('shop.admin.rules.level') }}</th>
              <th>{{ $t('shop.admin.rules.levelPercent') }}</th>
              <th>{{ $t('shop.admin.rules.percent') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in grid" :key="i" :class="{ bad: row.percent < 0 || row.percent > row.base }">
              <td>{{ row.base }} %</td>
              <td>{{ row.level }}</td>
              <td>{{ formatPercent(row.level_percent) }} %</td>
              <td>{{ formatPercent(row.percent) }} %</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import {
  adminCheckPerkRule,
  adminPerkRules,
  adminSavePerkRule,
  shopError,
  shopErrorText,
  type PerkGridRow,
  type PerkRule,
} from '../../api/shop'
import { formatPercent } from '../../utils/perk'

interface Draft {
  code: string
  title: string
  source: string
  isActive: boolean
  origin: 'SHIPPED' | 'OWN'
}

const TEMPLATE = `MANIFEST = {
    "title": "Комиссия умножается на VALUE",
    "description": "Что делает правило — для админки",
    "defaults": {"VALUE": 0.5},
}

# f.base — базовая ставка, f.level_percent — ставка по уровню,
# f.level — уровень, f.config — константы с настройками товара.
def rate(f):
    return f.level_percent * f.config["VALUE"]
`

// Правила привилегий (implementation_plan_delivery_passport.md §1): скрипт
// считает ставку, сервер проверяет его прогоном по сетке до сохранения.
export default defineComponent({
  name: 'ShopPerkRules',
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()
    const can = (permission: string) => authStore.can(permission)

    const rules = ref<PerkRule[]>([])
    const draft = ref<Draft | null>(null)
    const isNew = ref(false)
    const grid = ref<PerkGridRow[]>([])
    const fieldErrors = ref<Record<string, string>>({})
    const errorMsg = ref('')
    const successMsg = ref('')
    const saving = ref(false)
    const checking = ref(false)

    const canSave = computed(() => (isNew.value ? can('perk_rules.create') : can('perk_rules.edit')))

    const load = async () => {
      errorMsg.value = ''
      try {
        rules.value = await adminPerkRules()
      } catch (err) {
        errorMsg.value = shopErrorText(err, t, t('shop.loadFailed'))
      }
    }

    const reset = () => {
      grid.value = []
      fieldErrors.value = {}
      successMsg.value = ''
    }

    const startNew = () => {
      reset()
      isNew.value = true
      draft.value = { code: '', title: '', source: TEMPLATE, isActive: true, origin: 'OWN' }
    }

    const edit = (r: PerkRule) => {
      reset()
      isNew.value = false
      draft.value = { code: r.code, title: r.title, source: r.source, isActive: r.is_active, origin: r.origin }
    }

    // Поставляемое правило — готовый образец для собственного.
    const copyAsNew = () => {
      if (!draft.value) return
      const source = draft.value.source
      startNew()
      if (draft.value) draft.value.source = source
    }

    const showError = (err: unknown) => {
      const e = shopError(err)
      fieldErrors.value = e?.fields || {}
      const details = (e as { details?: { grid?: PerkGridRow[] } } | null)?.details
      grid.value = details?.grid || []
      errorMsg.value = shopErrorText(err, t, t('shop.errors.generic'))
    }

    const check = async () => {
      if (!draft.value) return
      checking.value = true
      errorMsg.value = ''
      fieldErrors.value = {}
      try {
        grid.value = (await adminCheckPerkRule(draft.value.source)).grid
      } catch (err) {
        showError(err)
      } finally {
        checking.value = false
      }
    }

    const save = async () => {
      if (!draft.value) return
      saving.value = true
      errorMsg.value = ''
      successMsg.value = ''
      fieldErrors.value = {}
      try {
        const d = draft.value
        const res = await adminSavePerkRule(
          { code: d.code.trim(), title: d.title.trim(), source: d.source, is_active: d.isActive },
          isNew.value,
        )
        await load()
        if (res.rule) edit(res.rule)
        grid.value = res.grid
        successMsg.value = t('shop.admin.saved')
      } catch (err) {
        showError(err)
      } finally {
        saving.value = false
      }
    }

    onMounted(load)

    return {
      can, rules, draft, isNew, grid, fieldErrors, errorMsg, successMsg, saving, checking, canSave,
      startNew, edit, copyAsNew, check, save, formatPercent,
    }
  },
})
</script>

<style scoped src="./shop-admin.css"></style>
<style scoped>
.source {
  width: 100%;
  font-size: 12px;
  line-height: 1.5;
}
.grid {
  margin-top: 14px;
  max-height: 320px;
  overflow: auto;
}
tr.bad td {
  color: #b91c1c;
}
</style>
