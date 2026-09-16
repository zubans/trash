<template>
  <div class="symbols-page">
    <header class="page-head">
      <p class="page-sub">
        Жесты, которые исполнитель показывает в кадре фото-подтверждения. Заказ получает случайный жест из
        действующих. Удалённый жест новым заказам не выдаётся, но остаётся у тех, кто его уже получил.
      </p>
      <div class="head-actions">
        <label class="toggle">
          <input v-model="includeDeleted" type="checkbox" @change="load" />
          Удалённые
        </label>
        <button v-if="canCreate" type="button" class="btn-primary" @click="startCreate">
          <i class="ph-bold ph-plus"></i> Жест
        </button>
      </div>
    </header>

    <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>
    <p v-if="successMsg" class="alert success">{{ successMsg }}</p>

    <form v-if="editing" class="editor" @submit.prevent="save">
      <h3>{{ editing.id ? `Жест №${editing.number}` : 'Новый жест' }}</h3>
      <div class="grid">
        <label>
          Код
          <input v-model="editing.code" required placeholder="thumb_up" pattern="[a-zA-Z][a-zA-Z0-9_]{1,31}" />
        </label>
        <label>
          Название
          <input v-model="editing.title" required placeholder="Поднятый большой палец" />
        </label>
        <label>
          Порядок
          <input v-model.number="editing.sort_order" type="number" />
        </label>
        <label>
          Картинка-подсказка (ссылка)
          <input v-model="editing.hint_image_url" placeholder="/uploads/... или https://..." />
        </label>
      </div>
      <label class="wide">
        Что показать — это исполнитель прочитает в попапе перед съёмкой
        <textarea v-model="editing.description" rows="3" maxlength="1000"></textarea>
      </label>
      <label class="check">
        <input v-model="editing.fits_in_selfie" type="checkbox" />
        Помещается в селфи с заказчиком (для жестов ногой — выключить)
      </label>
      <div class="editor-actions">
        <button type="button" class="btn-secondary" @click="editing = null">Отмена</button>
        <button type="submit" class="btn-primary" :disabled="saving">Сохранить</button>
      </div>
    </form>

    <div class="table-wrap">
      <table class="symbols">
        <thead>
          <tr>
            <th>№</th>
            <th>Жест</th>
            <th>Код</th>
            <th>В селфи</th>
            <th>Порядок</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="symbol in symbols" :key="symbol.id" :class="{ deleted: symbol.deleted_at }">
            <td>{{ symbol.number }}</td>
            <td>
              <div class="title">{{ symbol.title }}</div>
              <div class="desc">{{ symbol.description }}</div>
            </td>
            <td><code>{{ symbol.code }}</code></td>
            <td>{{ symbol.fits_in_selfie ? 'да' : 'нет' }}</td>
            <td>{{ symbol.sort_order }}</td>
            <td class="row-actions">
              <template v-if="!symbol.deleted_at">
                <button v-if="canEdit" type="button" title="Изменить" @click="startEdit(symbol)">
                  <i class="ph-bold ph-pencil-simple"></i>
                </button>
                <button v-if="canDelete" type="button" title="Удалить" class="danger" @click="remove(symbol)">
                  <i class="ph-bold ph-trash"></i>
                </button>
              </template>
              <button v-else-if="canEdit" type="button" title="Восстановить" @click="restore(symbol)">
                <i class="ph-bold ph-arrow-counter-clockwise"></i>
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, ref } from 'vue'
import {
  createSymbol,
  deleteSymbol,
  listSymbols,
  restoreSymbol,
  updateSymbol,
  type WatermarkSymbol,
} from '../../api/watermarks'
import { useAuthStore } from '../../stores/auth-store'

type Editing = Partial<WatermarkSymbol> & { code: string; title: string; description: string; fits_in_selfie: boolean; sort_order: number }

export default defineComponent({
  name: 'AdminWatermarkSymbols',
  setup() {
    const authStore = useAuthStore()
    const symbols = ref<WatermarkSymbol[]>([])
    const includeDeleted = ref(false)
    const editing = ref<Editing | null>(null)
    const saving = ref(false)
    const errorMsg = ref('')
    const successMsg = ref('')

    const canCreate = computed(() => authStore.can('watermarks.create'))
    const canEdit = computed(() => authStore.can('watermarks.edit'))
    const canDelete = computed(() => authStore.can('watermarks.delete'))

    const apiError = (err: any, fallback: string) => (typeof err.response?.data === 'string' ? err.response.data : fallback)

    const load = async () => {
      errorMsg.value = ''
      try {
        symbols.value = await listSymbols(includeDeleted.value)
      } catch (err: any) {
        errorMsg.value = apiError(err, 'Не удалось загрузить жесты')
      }
    }

    const startCreate = () => {
      const nextOrder = symbols.value.reduce((max, s) => Math.max(max, s.sort_order), 0) + 10
      editing.value = { code: '', title: '', description: '', hint_image_url: '', fits_in_selfie: true, sort_order: nextOrder }
    }

    const startEdit = (symbol: WatermarkSymbol) => {
      editing.value = { ...symbol, hint_image_url: symbol.hint_image_url || '' }
    }

    const save = async () => {
      if (!editing.value) return
      saving.value = true
      errorMsg.value = ''
      const { id, code, title, description, hint_image_url, fits_in_selfie, sort_order } = editing.value
      const input = { code, title, description, hint_image_url, fits_in_selfie, sort_order }
      try {
        if (id) await updateSymbol(id, input)
        else await createSymbol(input)
        successMsg.value = 'Жест сохранён'
        editing.value = null
        await load()
      } catch (err: any) {
        errorMsg.value = apiError(err, 'Не удалось сохранить жест')
      } finally {
        saving.value = false
      }
    }

    const remove = async (symbol: WatermarkSymbol) => {
      if (!window.confirm(`Удалить жест «${symbol.title}»? Новым заказам он выдаваться не будет.`)) return
      try {
        await deleteSymbol(symbol.id)
        successMsg.value = 'Жест удалён'
        await load()
      } catch (err: any) {
        errorMsg.value = apiError(err, 'Не удалось удалить жест')
      }
    }

    const restore = async (symbol: WatermarkSymbol) => {
      try {
        await restoreSymbol(symbol.id)
        successMsg.value = 'Жест восстановлен'
        await load()
      } catch (err: any) {
        errorMsg.value = apiError(err, 'Не удалось восстановить жест')
      }
    }

    onMounted(load)

    return {
      symbols,
      includeDeleted,
      editing,
      saving,
      errorMsg,
      successMsg,
      canCreate,
      canEdit,
      canDelete,
      load,
      startCreate,
      startEdit,
      save,
      remove,
      restore,
    }
  },
})
</script>

<style scoped>
.symbols-page { max-width: 1000px; margin: 0 auto; padding: 16px; color: #0f172a; }
.page-head { display: flex; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
.page-sub { margin: 6px 0 0; color: #64748b; font-size: 14px; line-height: 1.5; max-width: 640px; }
.head-actions { display: flex; gap: 10px; align-items: center; flex-shrink: 0; }
.toggle { display: flex; gap: 6px; align-items: center; font-size: 14px; color: #475569; }
.btn-primary, .btn-secondary { height: 40px; border-radius: 10px; border: none; padding: 0 14px; font-weight: 600; cursor: pointer; font-family: inherit; }
.btn-primary { background: #4f46e5; color: #fff; }
.btn-primary:disabled { opacity: 0.6; }
.btn-secondary { background: #f1f5f9; color: #475569; }
.alert { padding: 10px 14px; border-radius: 10px; font-size: 14px; margin: 0 0 16px; }
.alert.error { background: #fef2f2; color: #b91c1c; }
.alert.success { background: #f0fdf4; color: #15803d; }
.editor { border: 1px solid #e2e8f0; border-radius: 16px; padding: 16px; margin-bottom: 16px; background: #fff; }
.editor h3 { margin: 0 0 12px; font-size: 16px; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 10px; }
.editor label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: #64748b; font-weight: 600; }
.editor input:not([type='checkbox']), .editor textarea { border: 1px solid #e2e8f0; border-radius: 10px; padding: 8px 10px; font-size: 14px; font-family: inherit; color: #0f172a; }
.editor .wide { margin-top: 10px; }
.editor .check { flex-direction: row; align-items: center; gap: 8px; margin-top: 10px; color: #334155; }
.editor-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 12px; }
.table-wrap { overflow-x: auto; }
.symbols { width: 100%; border-collapse: collapse; background: #fff; border-radius: 12px; }
.symbols th, .symbols td { text-align: left; padding: 10px; border-bottom: 1px solid #f1f5f9; font-size: 14px; vertical-align: top; }
.symbols th { font-size: 12px; color: #64748b; text-transform: uppercase; letter-spacing: 0.4px; }
.symbols tr.deleted { opacity: 0.55; }
.title { font-weight: 600; }
.desc { font-size: 12px; color: #64748b; max-width: 420px; }
.row-actions { white-space: nowrap; }
.row-actions button { border: none; background: #f1f5f9; color: #475569; width: 34px; height: 34px; border-radius: 8px; cursor: pointer; margin-left: 4px; }
.row-actions button.danger { background: #fef2f2; color: #dc2626; }
@media (max-width: 640px) { .page-head { flex-direction: column; } }
</style>
