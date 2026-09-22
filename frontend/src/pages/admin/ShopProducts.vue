<template>
  <div class="shop-admin">
    <header class="page-head">
      <p class="page-sub">
        Товары витрины. Род решает, чем товар выдаётся: привилегия — сроком в очереди,
        вещь — купоном со склада подарка, сертификат — кодом из пула. Удаления нет:
        товар с продажами снимается с витрины.
      </p>
      <div class="toolbar">
        <router-link v-if="can('shop.view')" to="/admin/shop/pickup-points" class="btn-secondary">
          {{ $t('shop.admin.pickupPoints') }}
        </router-link>
        <button v-if="can('shop.create')" type="button" class="btn-primary" @click="startNew">
          <i class="ph-bold ph-plus"></i> {{ $t('shop.admin.newProduct') }}
        </button>
        <button type="button" class="btn-refresh" :disabled="loading" @click="load">
          <i class="ph-bold ph-arrows-clockwise"></i>
        </button>
      </div>
    </header>

    <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>
    <p v-if="successMsg" class="alert success">{{ successMsg }}</p>

    <section class="panel">
      <div class="row filters">
        <select v-model="filterKind" class="input">
          <option value="">{{ $t('shop.admin.all') }}</option>
          <option v-for="k in kinds" :key="k" :value="k">{{ $t('shop.kinds.' + k) }}</option>
        </select>
        <select v-model="filterCategory" class="input">
          <option value="">{{ $t('shop.admin.all') }}</option>
          <option v-for="c in categories" :key="c" :value="c">{{ c }}</option>
        </select>
      </div>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th></th>
              <th>{{ $t('shop.admin.fields.titleRu') }}</th>
              <th>{{ $t('shop.admin.fields.kind') }}</th>
              <th>{{ $t('shop.admin.fields.price') }}</th>
              <th>{{ $t('shop.admin.stock') }}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="p in filtered"
              :key="p.id"
              class="clickable"
              :class="{ selected: editingId === p.id }"
              @click="edit(p)"
            >
              <td>
                <img v-if="p.images.length" :src="imageUrl(p.images[0])" class="thumb" alt="" />
                <div v-else class="thumb"></div>
              </td>
              <td>
                {{ p.title?.ru || '—' }}
                <div class="muted">{{ p.category }}</div>
              </td>
              <td>
                {{ $t('shop.kinds.' + p.kind) }}
                <div v-if="p.kind === 'PERK'" class="muted">{{ ruleSummary(p.perk_rule, p.perk_config) }}, {{ p.perk_days }} дн.</div>
              </td>
              <td>{{ money(p.price) }}</td>
              <td>{{ stockLabel(p) }}</td>
              <td>
                <span class="badge" :class="p.is_active ? 'ok' : ''">
                  {{ p.is_active ? $t('shop.admin.active') : $t('shop.admin.inactive') }}
                </span>
              </td>
            </tr>
            <tr v-if="!filtered.length">
              <td colspan="6" class="empty">{{ $t('shop.admin.noProducts') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-if="draft" ref="formRef" class="panel">
      <h2>{{ editingId ? $t('shop.admin.editProduct') : $t('shop.admin.newProduct') }}</h2>
      <div class="form-grid">
        <label class="field">
          <span>{{ $t('shop.admin.fields.kind') }}</span>
          <select v-model="draft.kind" class="input" :class="{ invalid: fieldErrors.kind }">
            <option v-for="k in kinds" :key="k" :value="k">{{ $t('shop.kinds.' + k) }}</option>
          </select>
          <span v-if="fieldErrors.kind" class="field-error">{{ fieldErrors.kind }}</span>
        </label>
        <label class="field">
          <span>{{ $t('shop.admin.fields.category') }}</span>
          <input v-model="draft.category" class="input mono" list="shop-categories" placeholder="perks" :class="{ invalid: fieldErrors.category }" />
          <datalist id="shop-categories">
            <option value="perks"></option>
            <option value="merch"></option>
            <option value="certificates"></option>
          </datalist>
          <span v-if="fieldErrors.category" class="field-error">{{ fieldErrors.category }}</span>
        </label>
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
          <span>{{ $t('shop.admin.fields.descriptionRu') }}</span>
          <textarea v-model="draft.descriptionRu" class="input" rows="3"></textarea>
        </label>
        <label class="field wide">
          <span>{{ $t('shop.admin.fields.descriptionEn') }}</span>
          <textarea v-model="draft.descriptionEn" class="input" rows="2"></textarea>
        </label>
        <label class="field">
          <span>{{ $t('shop.admin.fields.price') }}, ₽</span>
          <input v-model.number="draft.price" type="number" min="0" step="0.01" class="input" :class="{ invalid: fieldErrors.price }" />
          <span v-if="fieldErrors.price" class="field-error">{{ fieldErrors.price }}</span>
        </label>
        <label class="field">
          <span>{{ $t('shop.admin.fields.compareAt') }}, ₽</span>
          <input v-model.number="draft.compareAt" type="number" min="0" step="0.01" class="input" :class="{ invalid: fieldErrors.compare_at_price }" />
          <span v-if="fieldErrors.compare_at_price" class="field-error">{{ fieldErrors.compare_at_price }}</span>
        </label>
        <label class="field">
          <span>{{ $t('shop.admin.fields.perUserLimit') }}</span>
          <input v-model.number="draft.perUserLimit" type="number" min="1" class="input" :class="{ invalid: fieldErrors.per_user_limit }" />
          <span v-if="fieldErrors.per_user_limit" class="field-error">{{ fieldErrors.per_user_limit }}</span>
        </label>
        <label class="field">
          <span>{{ $t('shop.admin.fields.sortOrder') }}</span>
          <input v-model.number="draft.sortOrder" type="number" class="input" />
        </label>

        <!-- Привилегия: правило из справочника и его константы. Что константы
             значат, знает правило; сервер проверяет товар прогоном по сетке. -->
        <template v-if="draft.kind === 'PERK'">
          <label class="field">
            <span>{{ $t('shop.admin.fields.perkRule') }}</span>
            <select v-model="draft.perkRule" class="input" :class="{ invalid: fieldErrors.perk_rule }" @change="resetPerkConfig">
              <option v-for="r in perkRuleOptions" :key="r.code" :value="r.code">{{ r.title }}</option>
            </select>
            <span v-if="selectedRule?.description" class="muted">{{ selectedRule.description }}</span>
            <span v-if="fieldErrors.perk_rule" class="field-error">{{ fieldErrors.perk_rule }}</span>
          </label>
          <label v-for="key in Object.keys(draft.perkConfig)" :key="key" class="field">
            <span>{{ key }}</span>
            <input v-model.number="draft.perkConfig[key]" type="number" step="any" class="input" :class="{ invalid: fieldErrors.perk_config }" />
          </label>
          <span v-if="fieldErrors.perk_config" class="field-error">{{ fieldErrors.perk_config }}</span>
          <label class="field">
            <span>{{ $t('shop.admin.fields.perkDays') }}</span>
            <input v-model.number="draft.perkDays" type="number" min="1" class="input" :class="{ invalid: fieldErrors.perk_days }" />
            <span v-if="fieldErrors.perk_days" class="field-error">{{ fieldErrors.perk_days }}</span>
          </label>
          <label class="field">
            <span>{{ $t('shop.admin.fields.maxQueued') }}</span>
            <input v-model.number="draft.maxQueued" type="number" min="1" placeholder="3" class="input" :class="{ invalid: fieldErrors.max_active_per_user }" />
            <span v-if="fieldErrors.max_active_per_user" class="field-error">{{ fieldErrors.max_active_per_user }}</span>
          </label>
        </template>

        <!-- Вещь и сертификат: подарок, склад которого общий с ачивками. -->
        <template v-else>
          <label class="field">
            <span>{{ $t('shop.admin.fields.gift') }}</span>
            <select v-model="draft.giftCode" class="input" :class="{ invalid: fieldErrors.gift_code }">
              <option value="">—</option>
              <option v-for="g in giftsOfKind" :key="g.code" :value="g.code">
                {{ g.title?.ru || g.code }} ({{ giftStock(g) }})
              </option>
            </select>
            <span v-if="fieldErrors.gift_code" class="field-error">{{ fieldErrors.gift_code }}</span>
            <router-link to="/admin/gifts" class="btn-link">{{ $t('shop.admin.giftsLink') }}</router-link>
          </label>
          <template v-if="draft.kind === 'PHYSICAL'">
            <label class="field">
              <span>{{ $t('shop.admin.fields.maxQty') }}</span>
              <input v-model.number="draft.maxQty" type="number" min="1" class="input" />
            </label>
            <label class="field">
              <span>{{ $t('shop.admin.fields.variants') }}</span>
              <input v-model="draft.variants" class="input mono" placeholder="S, M, L, XL" :class="{ invalid: fieldErrors.variants }" />
              <span v-if="fieldErrors.variants" class="field-error">{{ fieldErrors.variants }}</span>
            </label>
            <div class="field">
              <span>{{ $t('shop.admin.fields.methods') }}</span>
              <label v-for="m in methods" :key="m" class="field checkbox">
                <input v-model="draft.methods" type="checkbox" :value="m" /> {{ $t('shop.methods.' + m) }}
              </label>
              <span v-if="fieldErrors.fulfillment_methods" class="field-error">{{ fieldErrors.fulfillment_methods }}</span>
            </div>
          </template>
        </template>

        <div class="field wide">
          <span>{{ $t('shop.admin.fields.roles') }}</span>
          <div class="row">
            <label v-for="r in roles" :key="r.code" class="field checkbox">
              <input v-model="draft.roles" type="checkbox" :value="r.code" /> {{ r.name || r.code }}
            </label>
          </div>
          <span v-if="fieldErrors.roles" class="field-error">{{ fieldErrors.roles }}</span>
        </div>

        <div class="field wide">
          <span>{{ $t('shop.admin.fields.images') }}</span>
          <div class="images">
            <div v-for="(img, i) in draft.images" :key="img" class="image-item">
              <img :src="imageUrl(img)" alt="" />
              <div class="image-actions">
                <button type="button" class="btn-link" :disabled="i === 0" @click="moveImage(i, -1)">↑</button>
                <button type="button" class="btn-link" :disabled="i === draft.images.length - 1" @click="moveImage(i, 1)">↓</button>
                <button type="button" class="btn-link danger" @click="draft.images.splice(i, 1)">✕</button>
              </div>
            </div>
            <label v-if="draft.images.length < 5" class="upload">
              <input type="file" accept="image/jpeg,image/png,image/webp,image/gif" :disabled="uploading" @change="upload" />
              <i class="ph-bold ph-upload-simple"></i> {{ $t('shop.admin.upload') }}
            </label>
          </div>
          <span v-if="fieldErrors.images" class="field-error">{{ fieldErrors.images }}</span>
        </div>

        <label class="field checkbox">
          <input v-model="draft.requiresVerified" type="checkbox" /> {{ $t('shop.admin.fields.requiresVerified') }}
        </label>
        <label class="field checkbox">
          <input v-model="draft.isActive" type="checkbox" /> {{ $t('shop.admin.fields.isActive') }}
        </label>
      </div>

      <!-- Предпросмотр — так карточка выглядит на витрине. -->
      <h3>{{ $t('shop.admin.preview') }}</h3>
      <div class="preview">
        <div class="preview-image">
          <img v-if="draft.images.length" :src="imageUrl(draft.images[0])" alt="" />
          <i v-else class="ph-fill ph-bag"></i>
        </div>
        <div class="preview-body">
          <div class="preview-title">{{ draft.titleRu || '—' }}</div>
          <div v-if="draft.kind === 'PERK'" class="muted">
            {{ ruleText(selectedRule?.title, draft.perkConfig) }} · {{ draft.perkDays || '?' }} дн.
          </div>
          <div class="preview-price">
            {{ money(draft.price || 0) }}
            <s v-if="draft.compareAt" class="muted">{{ money(draft.compareAt) }}</s>
          </div>
        </div>
      </div>

      <div class="row actions">
        <button type="button" class="btn-primary" :disabled="saving || !canSave" @click="confirmSave">
          {{ $t('common.save') }}
        </button>
        <button type="button" class="btn-secondary" @click="cancelEdit">{{ $t('common.cancel') }}</button>
      </div>
    </section>

    <va-modal
      v-model="showPriceConfirm"
      :message="priceConfirmText"
      :ok-text="$t('common.confirm')"
      :cancel-text="$t('common.cancel')"
      @ok="save"
    />
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import { resolveFileUrl } from '../../services/api'
import { adminGetGifts, type Gift } from '../../api/achievements'
import { getRoles, type Role } from '../../api/roles'
import {
  adminGetProducts,
  adminPerkRules,
  adminSaveProduct,
  adminUploadImage,
  shopError,
  shopErrorText,
  type FulfillmentMethod,
  type PerkConfig,
  type PerkRule,
  type ProductPayload,
  type ShopKind,
  type ShopProduct,
} from '../../api/shop'
import { ruleSummary, ruleText } from '../../utils/perk'

interface Draft {
  kind: ShopKind
  category: string
  titleRu: string
  titleEn: string
  descriptionRu: string
  descriptionEn: string
  price: number
  compareAt: number | null
  roles: string[]
  requiresVerified: boolean
  perUserLimit: number | null
  maxQty: number
  giftCode: string
  variants: string
  methods: FulfillmentMethod[]
  perkRule: string
  perkConfig: PerkConfig
  perkDays: number | null
  maxQueued: number | null
  sortOrder: number
  isActive: boolean
  images: string[]
}

const emptyDraft = (): Draft => ({
  kind: 'PERK', category: 'perks', titleRu: '', titleEn: '', descriptionRu: '', descriptionEn: '',
  price: 0, compareAt: null, roles: ['EXECUTOR'], requiresVerified: false, perUserLimit: null, maxQty: 1,
  giftCode: '', variants: '', methods: ['PICKUP'], perkRule: '', perkConfig: {},
  perkDays: 30, maxQueued: null, sortOrder: 0, isActive: false, images: [],
})

const FALLBACK_ROLES = ['CUSTOMER', 'EXECUTOR', 'MODERATOR'].map((code) => ({ code, name: code }) as Role)

// Пустое поле числа в форме — это «не задано», а не ноль.
const optionalNumber = (value: number | null | string): number | undefined =>
  value === null || value === '' || value === undefined || Number.isNaN(Number(value)) ? undefined : Number(value)

export default defineComponent({
  name: 'ShopProducts',
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()
    const can = (permission: string) => authStore.can(permission)

    const kinds: ShopKind[] = ['PERK', 'PHYSICAL', 'CERTIFICATE']
    const methods: FulfillmentMethod[] = ['PICKUP', 'DELIVERY']

    const products = ref<ShopProduct[]>([])
    const gifts = ref<(Gift & { free_codes: number })[]>([])
    const roles = ref<Role[]>([])
    const perkRules = ref<PerkRule[]>([])
    const loading = ref(false)
    const saving = ref(false)
    const uploading = ref(false)
    const errorMsg = ref('')
    const successMsg = ref('')
    const filterKind = ref('')
    const filterCategory = ref('')
    const draft = ref<Draft | null>(null)
    const editingId = ref<string | null>(null)
    const editingOriginal = ref<ShopProduct | null>(null)
    const fieldErrors = ref<Record<string, string>>({})
    const showPriceConfirm = ref(false)
    const formRef = ref<HTMLElement | null>(null)

    const money = (value: number) => `${Number(value || 0).toLocaleString('ru-RU', { maximumFractionDigits: 2 })} ₽`
    const imageUrl = (path: string) => resolveFileUrl(path)

    const categories = computed(() => [...new Set(products.value.map((p) => p.category))])
    const filtered = computed(() =>
      products.value.filter(
        (p) => (!filterKind.value || p.kind === filterKind.value) && (!filterCategory.value || p.category === filterCategory.value),
      ),
    )
    // В списке — включённые правила и то, на котором товар уже стоит, даже
    // если его выключили: иначе форма молча подменила бы правило.
    const perkRuleOptions = computed(() =>
      perkRules.value.filter((r) => r.is_active || r.code === draft.value?.perkRule),
    )
    const selectedRule = computed(() => perkRules.value.find((r) => r.code === draft.value?.perkRule))
    // Смена правила — его константы по умолчанию: у другого правила они другие.
    const resetPerkConfig = () => {
      if (draft.value) draft.value.perkConfig = { ...(selectedRule.value?.defaults || {}) }
    }

    const giftsOfKind = computed(() => gifts.value.filter((g) => g.kind === draft.value?.kind))

    const giftStock = (g: Gift & { free_codes: number }) =>
      g.kind === 'CERTIFICATE'
        ? t('shop.admin.freeCodes', { count: g.free_codes })
        : g.stock === undefined || g.stock === null
          ? t('shop.admin.unlimited')
          : String(g.stock)
    const stockLabel = (p: ShopProduct) => {
      if (p.kind === 'PERK') return t('shop.admin.unlimited')
      if (p.stock_count === undefined || p.stock_count === null) return t('shop.admin.unlimited')
      return String(p.stock_count)
    }

    const load = async () => {
      loading.value = true
      errorMsg.value = ''
      try {
        // Подарки и роли охраняются своими правами. Роль, которой дали только
        // «Товары магазина», всё равно должна видеть список: без справочников
        // форма предлагает системные роли и пустой список подарков.
        const [list, giftList, roleList, ruleList] = await Promise.all([
          adminGetProducts(),
          adminGetGifts().catch(() => []),
          getRoles().catch(() => FALLBACK_ROLES),
          adminPerkRules().catch(() => []),
        ])
        products.value = list
        gifts.value = giftList
        roles.value = roleList
        perkRules.value = ruleList
      } catch (err) {
        errorMsg.value = shopErrorText(err, t, t('shop.loadFailed'))
      } finally {
        loading.value = false
      }
    }

    const scrollToForm = () => nextTick(() => formRef.value?.scrollIntoView({ behavior: 'smooth', block: 'start' }))

    const startNew = () => {
      editingId.value = null
      editingOriginal.value = null
      draft.value = emptyDraft()
      const first = perkRules.value.find((r) => r.is_active)
      if (first) {
        draft.value.perkRule = first.code
        resetPerkConfig()
      }
      fieldErrors.value = {}
      successMsg.value = ''
      scrollToForm()
    }

    const edit = (p: ShopProduct) => {
      if (!can('shop.edit')) return
      editingId.value = p.id
      editingOriginal.value = p
      fieldErrors.value = {}
      successMsg.value = ''
      draft.value = {
        kind: p.kind, category: p.category, titleRu: p.title?.ru || '', titleEn: p.title?.en || '',
        descriptionRu: p.description?.ru || '', descriptionEn: p.description?.en || '',
        price: p.price, compareAt: p.compare_at_price ?? null, roles: [...(p.roles || [])],
        requiresVerified: p.requires_verified, perUserLimit: p.per_user_limit ?? null, maxQty: p.max_qty_per_order,
        giftCode: p.gift_code || '', variants: (p.variants || []).map((v) => v.code).join(', '),
        methods: [...(p.fulfillment_methods || [])], perkRule: p.perk_rule || '',
        perkConfig: { ...(p.perk_config || {}) }, perkDays: p.perk_days ?? null, maxQueued: p.max_active_per_user ?? null,
        sortOrder: p.sort_order, isActive: p.is_active, images: [...(p.images || [])],
      }
      scrollToForm()
    }

    const cancelEdit = () => {
      draft.value = null
      editingId.value = null
      editingOriginal.value = null
    }

    const canSave = computed(() => (editingId.value ? can('shop.edit') : can('shop.create')))

    // Поля чужого рода в запрос не попадают: сервер их не примет.
    const payload = (d: Draft): ProductPayload => {
      const base = {
        kind: d.kind, category: d.category.trim(),
        title: { ru: d.titleRu.trim(), ...(d.titleEn.trim() ? { en: d.titleEn.trim() } : {}) },
        description: { ...(d.descriptionRu.trim() ? { ru: d.descriptionRu.trim() } : {}), ...(d.descriptionEn.trim() ? { en: d.descriptionEn.trim() } : {}) },
        images: d.images, price: Number(d.price) || 0, compare_at_price: optionalNumber(d.compareAt),
        roles: d.roles, requires_verified: d.requiresVerified, per_user_limit: optionalNumber(d.perUserLimit),
        sort_order: Number(d.sortOrder) || 0, is_active: d.isActive,
        max_qty_per_order: 1, variants: [], fulfillment_methods: [],
      } as ProductPayload
      if (d.kind === 'PERK') {
        return {
          ...base, perk_rule: d.perkRule || undefined, perk_config: d.perkConfig,
          perk_days: optionalNumber(d.perkDays), max_active_per_user: optionalNumber(d.maxQueued),
        }
      }
      const withGift = { ...base, gift_code: d.giftCode || undefined }
      if (d.kind === 'CERTIFICATE') return withGift
      return {
        ...withGift,
        max_qty_per_order: Math.max(1, Number(d.maxQty) || 1),
        variants: d.variants.split(',').map((c) => c.trim()).filter(Boolean).map((code) => ({ code })),
        fulfillment_methods: d.methods,
      }
    }

    // Смена цены активного товара — с подтверждением: у кого окно оформления
    // открыто, получат «цена изменилась».
    const priceConfirmText = computed(() =>
      editingOriginal.value && draft.value
        ? t('shop.admin.priceConfirm', { from: money(editingOriginal.value.price), to: money(draft.value.price) })
        : '',
    )
    const confirmSave = () => {
      const original = editingOriginal.value
      if (original && original.is_active && draft.value && Number(draft.value.price) !== original.price) {
        showPriceConfirm.value = true
        return
      }
      save()
    }

    const save = async () => {
      if (!draft.value) return
      saving.value = true
      errorMsg.value = ''
      successMsg.value = ''
      fieldErrors.value = {}
      try {
        const saved = await adminSaveProduct(editingId.value, payload(draft.value))
        await load()
        edit(saved)
        // После edit: он сбрасывает сообщение прошлого сохранения.
        successMsg.value = t('shop.admin.saved')
      } catch (err) {
        const e = shopError(err)
        fieldErrors.value = e?.fields || {}
        errorMsg.value = shopErrorText(err, t, t('shop.errors.generic'))
      } finally {
        saving.value = false
        showPriceConfirm.value = false
      }
    }

    const upload = async (event: Event) => {
      const input = event.target as HTMLInputElement
      const file = input.files?.[0]
      if (!file || !draft.value) return
      uploading.value = true
      try {
        const url = await adminUploadImage(file, file.name)
        if (url) draft.value.images.push(url)
      } catch (err) {
        fieldErrors.value = { ...fieldErrors.value, images: shopErrorText(err, t, t('shop.errors.generic')) }
      } finally {
        uploading.value = false
        input.value = ''
      }
    }

    const moveImage = (index: number, delta: number) => {
      if (!draft.value) return
      const list = draft.value.images
      const [img] = list.splice(index, 1)
      list.splice(index + delta, 0, img)
    }

    onMounted(load)

    return {
      can, kinds, methods, perkRuleOptions, selectedRule, resetPerkConfig, products, roles, loading, saving, uploading, errorMsg, successMsg,
      filterKind, filterCategory, draft, editingId, fieldErrors, showPriceConfirm, formRef,
      money, imageUrl, categories, filtered, giftsOfKind, giftStock, stockLabel, load, startNew, edit,
      cancelEdit, canSave, priceConfirmText, confirmSave, save, upload, moveImage, ruleSummary, ruleText,
    }
  },
})
</script>

<style scoped src="./shop-admin.css"></style>
<style scoped>
.filters {
  margin-bottom: 10px;
}
.images {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.image-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
}
.image-item img {
  width: 88px;
  height: 66px;
  border-radius: 8px;
  object-fit: cover;
}
.image-actions {
  display: flex;
  gap: 8px;
}
.danger {
  color: #b91c1c;
}
.upload {
  width: 88px;
  height: 66px;
  border: 1px dashed #cbd5e1;
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  font-size: 12px;
  color: #4b5563;
  cursor: pointer;
}
.upload input {
  display: none;
}
.preview {
  width: 200px;
  border-radius: 16px;
  overflow: hidden;
  border: 1px solid #eef0f4;
  margin-bottom: 12px;
}
.preview-image {
  aspect-ratio: 4 / 3;
  background: #eef2ff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 32px;
  color: #6366f1;
}
.preview-image img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.preview-body {
  padding: 8px 10px 10px;
  font-size: 13px;
}
.preview-title {
  font-weight: 600;
}
.preview-price {
  font-weight: 700;
  margin-top: 2px;
}
.actions {
  margin-top: 4px;
}
</style>
