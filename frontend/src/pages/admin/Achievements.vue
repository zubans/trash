<template>
  <div class="achievements-admin">
    <header class="page-head">
      <div>
        <h1>Ачивки</h1>
        <p class="page-sub">
          Ачивки бывают двух происхождений. Поставляемая приезжает со сборкой:
          её правило прошло ревью, и править скрипт отсюда нельзя — только
          включить, выключить и настроить. Собственную пишут здесь, скриптом, и
          её можно создать, отредактировать и удалить.
        </p>
      </div>
      <div class="head-actions">
        <label class="toggle">
          <input v-model="showArchived" type="checkbox" @change="load" />
          <span>архив</span>
        </label>
        <button type="button" class="btn-primary" @click="startCreate">
          <i class="ph-bold ph-plus"></i> Новая ачивка
        </button>
        <button type="button" class="btn-refresh" :disabled="loading" @click="load">
          <i class="ph-bold ph-arrows-clockwise"></i>
        </button>
      </div>
    </header>

    <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>
    <p v-if="successMsg" class="alert success">{{ successMsg }}</p>

    <!-- Редактор новой ачивки. Скрипт обязателен: ачивка без правила — это
         строка, которая никогда не сработает. -->
    <section v-if="creating" class="panel editor">
      <h2>Новая ачивка</h2>
      <div class="fields">
        <label class="field">
          <span>Код</span>
          <input v-model="draft.code" class="input mono" placeholder="fast_delivery" />
          <small>Строчные латинские буквы, цифры и подчёркивание.</small>
        </label>
        <label class="field">
          <span>Вес, баллов</span>
          <input v-model.number="draft.weight" type="number" min="0" max="10000" class="input" />
          <small>Пусто — вес из скрипта.</small>
        </label>
        <label class="field">
          <span>Доступна с</span>
          <input v-model="draft.available_from" type="datetime-local" class="input" />
        </label>
        <label class="field">
          <span>Доступна до</span>
          <input v-model="draft.available_to" type="datetime-local" class="input" />
        </label>
      </div>

      <label class="field">
        <span>Шаблон</span>
        <select class="input" @change="applyTemplate(($event.target as HTMLSelectElement).value)">
          <option value="">— пустой —</option>
          <option v-for="item in templates" :key="item.code" :value="item.code">
            {{ item.title || item.code }}
          </option>
        </select>
        <small>Копирует скрипт поставляемой ачивки как отправную точку.</small>
      </label>

      <label class="field">
        <span>config.star — константы</span>
        <textarea v-model="draft.constants" class="code" rows="8"></textarea>
      </label>
      <label class="field">
        <span>achievement.star — MANIFEST и хуки</span>
        <textarea v-model="draft.source" class="code" rows="18"></textarea>
      </label>

      <div class="editor-foot">
        <label class="toggle">
          <input v-model="draft.is_active" type="checkbox" />
          <span>включить сразу</span>
        </label>
        <button type="button" class="btn-primary" :disabled="saving === '#new'" @click="create">
          Создать
        </button>
        <button type="button" class="btn-link" @click="creating = false">Отмена</button>
      </div>
    </section>

    <p v-if="!loading && !items.length" class="empty">
      {{ showArchived ? 'Архив пуст.' : 'Ни одной ачивки не заведено.' }}
    </p>

    <div v-for="item in items" :key="item.code" class="achievement-card">
      <div class="card-head">
        <div>
          <div class="card-title">
            {{ item.title || item.code }}
            <span class="code-badge">{{ item.code }}</span>
            <span v-if="item.is_library" class="badge">поставляемая</span>
            <span v-else class="badge own">своя</span>
            <span v-if="!item.script_loaded" class="badge danger">скрипт не загружен</span>
            <span v-else-if="item.repeatable" class="badge">повторяемая</span>
          </div>
          <div class="card-sub">{{ item.description }}</div>
          <div class="card-meta">
            аудитория {{ item.audience || '—' }} · события: {{ item.events?.join(', ') || '—' }}
          </div>
        </div>

        <div v-if="item.deleted_at" class="head-actions">
          <button type="button" class="btn-link" @click="restore(item)">Восстановить</button>
        </div>
        <label v-else class="switch">
          <input
            type="checkbox"
            :checked="item.is_active"
            :disabled="!item.script_loaded"
            @change="toggle(item, ($event.target as HTMLInputElement).checked)"
          />
          <span>{{ item.is_active ? 'включена' : 'выключена' }}</span>
        </label>
      </div>

      <p v-if="!item.script_loaded" class="script-warning">
        Скрипт «{{ item.code }}» не скомпилировался или отсутствует. Включить
        ачивку нельзя: она никогда не сработает.
      </p>

      <template v-if="!item.deleted_at">
        <div class="fields">
          <label class="field">
            <span>Вес, баллов</span>
            <input
              v-model.number="drafts[item.code].weight"
              type="number"
              min="0"
              max="10000"
              class="input"
              :placeholder="String(item.script_weight || '')"
            />
            <small>Пусто — вес из скрипта ({{ item.script_weight || '—' }}).</small>
          </label>
          <label class="field">
            <span>Доступна с</span>
            <input v-model="drafts[item.code].available_from" type="datetime-local" class="input" />
          </label>
          <label class="field">
            <span>Доступна до</span>
            <input v-model="drafts[item.code].available_to" type="datetime-local" class="input" />
            <small>Пусто с обеих сторон — выдаётся всегда.</small>
          </label>
          <label class="field">
            <span>Порядок</span>
            <input v-model.number="drafts[item.code].sort_order" type="number" class="input" />
          </label>
        </div>

        <!-- Скрипт своей ачивки правится здесь же; поставляемый показывается
             только на чтение. -->
        <div v-if="openSource === item.code" class="source">
          <template v-if="item.is_library">
            <p class="hint">
              Скрипт приехал со сборкой и правится в репозитории
              (<code>backend/achievements/{{ item.code }}</code>). Скопируйте его в новую
              ачивку, если нужен вариант.
            </p>
            <h4>config.star</h4>
            <pre>{{ item.constants_source || '—' }}</pre>
            <h4>achievement.star</h4>
            <pre>{{ item.source_text || '—' }}</pre>
          </template>
          <template v-else>
            <label class="field">
              <span>config.star — константы</span>
              <textarea v-model="drafts[item.code].constants" class="code" rows="8"></textarea>
            </label>
            <label class="field">
              <span>achievement.star — MANIFEST и хуки</span>
              <textarea v-model="drafts[item.code].source" class="code" rows="18"></textarea>
            </label>
          </template>
        </div>

        <div class="card-foot">
          <span class="effective">
            Следующая выдача принесёт {{ effectiveWeight(item) }} балл(ов)
          </span>
          <button type="button" class="btn-link" @click="toggleSource(item.code)">
            {{ openSource === item.code ? 'Скрыть скрипт' : item.is_library ? 'Показать скрипт' : 'Редактировать скрипт' }}
          </button>
          <button
            v-if="!item.is_library"
            type="button"
            class="btn-danger"
            @click="remove(item)"
          >
            Удалить
          </button>
          <button type="button" class="btn-save" :disabled="saving === item.code" @click="save(item)">
            <span v-if="saving === item.code">Сохраняем…</span>
            <template v-else>Сохранить</template>
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, reactive, ref } from 'vue'

import {
  adminCreateAchievement,
  adminDeleteAchievement,
  adminGetAchievements,
  adminRestoreAchievement,
  adminSaveAchievement,
  type AchievementPayload,
  type AdminAchievement,
} from '../../api/achievements'

interface Draft {
  weight: number | null
  available_from: string
  available_to: string
  sort_order: number
  constants: string
  source: string
}

// Поле datetime-local не понимает ISO с зоной, а сервер отдаёт именно его.
const toLocalInput = (value?: string) => (value ? value.slice(0, 16) : '')
const fromLocalInput = (value: string) => (value ? new Date(value).toISOString() : null)

// Пустая заготовка: не пример из головы, а минимум, который компилируется и
// сразу показывает, из чего ачивка состоит.
const BLANK_SOURCE = `MANIFEST = {
    "title": "Название ачивки",
    "description": "Условие, за которое она выдаётся.",
    "icon": "star",
    # EXECUTOR или CUSTOMER — кому она адресована.
    "audience": "EXECUTOR",
    # На какие события пересчитывать. Событие вне списка не доставляется.
    "events": ["order.confirmed"],
    # False — повторяемая; тогда check обязан вернуть key.
    "once_per_user": True,
    "weight": 10,
}

def check(f):
    o = f.order
    if o == None or f.user == None:
        return None
    if o.executor_id != f.user.id:
        return None
    # Условие ачивки. f.stats несёт агрегаты исполнителя, f.order — заказ.
    if f.stats.orders_completed < 10:
        return None
    return grant(
        points = MANIFEST["weight"],
        order_id = o.id,
        effects = [notify(text = "Ачивка получена!")],
    )
`

export default defineComponent({
  name: 'AdminAchievements',
  setup() {
    const items = ref<AdminAchievement[]>([])
    const drafts = reactive<Record<string, Draft>>({})
    const loading = ref(false)
    const saving = ref('')
    const openSource = ref('')
    const showArchived = ref(false)
    const errorMsg = ref('')
    const successMsg = ref('')

    const creating = ref(false)
    const draft = reactive({
      code: '',
      weight: null as number | null,
      available_from: '',
      available_to: '',
      is_active: false,
      constants: '',
      source: BLANK_SOURCE,
    })

    // Шаблоны — поставляемые ачивки: их скрипт админ копирует как отправную
    // точку, а не переписывает у самой поставляемой.
    const templates = computed(() => items.value.filter((item) => item.is_library && item.script_loaded))

    const load = async () => {
      loading.value = true
      errorMsg.value = ''
      try {
        items.value = await adminGetAchievements(showArchived.value)
        items.value.forEach((item) => {
          drafts[item.code] = {
            weight: item.weight ?? null,
            available_from: toLocalInput(item.available_from),
            available_to: toLocalInput(item.available_to),
            sort_order: item.sort_order,
            constants: item.constants ?? '',
            source: item.source ?? '',
          }
        })
      } catch {
        errorMsg.value = 'Не удалось загрузить список ачивок.'
      } finally {
        loading.value = false
      }
    }

    const failWith = (e: unknown, fallback: string) => {
      const message = (e as { response?: { data?: string } })?.response?.data
      // Ошибка компиляции приходит текстом Starlark — с файлом, строкой и
      // сутью. Её и показываем как есть, а не «не удалось сохранить».
      errorMsg.value = (typeof message === 'string' && message.trim()) || fallback
    }

    const payloadFor = (item: AdminAchievement, isActive: boolean): AchievementPayload => {
      const d = drafts[item.code]
      return {
        is_active: isActive,
        available_from: fromLocalInput(d.available_from),
        available_to: fromLocalInput(d.available_to),
        weight: d.weight === null || d.weight === undefined ? null : Number(d.weight),
        config: item.config ?? {},
        sort_order: d.sort_order ?? 0,
        constants: d.constants,
        source: d.source,
      }
    }

    const persist = async (item: AdminAchievement, isActive: boolean) => {
      saving.value = item.code
      errorMsg.value = ''
      successMsg.value = ''
      try {
        await adminSaveAchievement(item.code, payloadFor(item, isActive))
        successMsg.value = `Ачивка «${item.title || item.code}» сохранена.`
        await load()
      } catch (e) {
        failWith(e, 'Не удалось сохранить.')
      } finally {
        saving.value = ''
      }
    }

    const save = (item: AdminAchievement) => persist(item, item.is_active)
    const toggle = (item: AdminAchievement, next: boolean) => persist(item, next)

    const startCreate = () => {
      creating.value = true
      errorMsg.value = ''
      draft.code = ''
      draft.weight = null
      draft.available_from = ''
      draft.available_to = ''
      draft.is_active = false
      draft.constants = ''
      draft.source = BLANK_SOURCE
    }

    const applyTemplate = (code: string) => {
      const template = items.value.find((item) => item.code === code)
      if (!template) {
        draft.constants = ''
        draft.source = BLANK_SOURCE
        return
      }
      draft.constants = template.constants_source ?? ''
      draft.source = template.source_text ?? BLANK_SOURCE
    }

    const create = async () => {
      saving.value = '#new'
      errorMsg.value = ''
      successMsg.value = ''
      try {
        await adminCreateAchievement({
          code: draft.code.trim(),
          is_active: draft.is_active,
          available_from: fromLocalInput(draft.available_from),
          available_to: fromLocalInput(draft.available_to),
          weight: draft.weight === null ? null : Number(draft.weight),
          constants: draft.constants,
          source: draft.source,
        })
        successMsg.value = `Ачивка «${draft.code}» создана.`
        creating.value = false
        await load()
      } catch (e) {
        failWith(e, 'Не удалось создать ачивку.')
      } finally {
        saving.value = ''
      }
    }

    const remove = async (item: AdminAchievement) => {
      // Предупреждение честное: выдачи и баллы остаются, и уровень у людей не
      // упадёт от того, что ачивку убрали из списка.
      const confirmed = window.confirm(
        `Убрать ачивку «${item.title || item.code}» в архив?\n\n` +
          'Она перестанет выдаваться. Уже выданные экземпляры и начисленные ' +
          'по ним баллы останутся: они — чей-то уровень и чья-то ставка комиссии.',
      )
      if (!confirmed) return
      try {
        await adminDeleteAchievement(item.code)
        successMsg.value = `Ачивка «${item.code}» в архиве.`
        await load()
      } catch (e) {
        failWith(e, 'Не удалось удалить.')
      }
    }

    const restore = async (item: AdminAchievement) => {
      try {
        await adminRestoreAchievement(item.code)
        successMsg.value = `Ачивка «${item.code}» восстановлена — выключенной.`
        await load()
      } catch (e) {
        failWith(e, 'Не удалось восстановить.')
      }
    }

    // Показывает то же, что посчитает сервер: свой вес, иначе вес скрипта.
    const effectiveWeight = (item: AdminAchievement) => {
      const d = drafts[item.code]
      if (d?.weight) return d.weight
      return item.script_weight || item.effective_weight
    }

    const toggleSource = (code: string) => {
      openSource.value = openSource.value === code ? '' : code
    }

    onMounted(load)

    return {
      items,
      drafts,
      loading,
      saving,
      openSource,
      showArchived,
      errorMsg,
      successMsg,
      creating,
      draft,
      templates,
      load,
      save,
      toggle,
      startCreate,
      applyTemplate,
      create,
      remove,
      restore,
      effectiveWeight,
      toggleSource,
    }
  },
})
</script>

<style scoped>
.achievements-admin {
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
  margin: 0 0 12px;
}

.page-sub {
  color: #6b7280;
  font-size: 13px;
  margin: 0;
  max-width: 640px;
}

.head-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  white-space: nowrap;
}

.btn-refresh {
  border: none;
  background: #fff;
  border-radius: 10px;
  padding: 8px 12px;
  cursor: pointer;
}

.toggle {
  font-size: 12px;
  color: #6b7280;
  display: flex;
  align-items: center;
  gap: 6px;
}

.alert {
  padding: 10px 12px;
  border-radius: 10px;
  font-size: 13px;
  margin-bottom: 12px;
  white-space: pre-wrap;
}

.alert.error {
  background: #fef2f2;
  color: #b91c1c;
  font-family: ui-monospace, monospace;
  font-size: 12px;
}

.alert.success {
  background: #ecfdf5;
  color: #047857;
}

.empty {
  color: #6b7280;
  font-size: 14px;
}

.panel,
.achievement-card {
  background: #fff;
  border-radius: 14px;
  padding: 16px;
  margin-bottom: 12px;
  border: 1px solid #eef0f4;
}

.panel.editor {
  border-color: #bfdbfe;
}

.card-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
}

.card-title {
  font-weight: 600;
  font-size: 15px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.code-badge {
  font-family: ui-monospace, monospace;
  font-size: 11px;
  color: #6b7280;
  background: #f3f4f6;
  padding: 2px 6px;
  border-radius: 6px;
}

.badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 999px;
  background: #f3f4f6;
  color: #4b5563;
}

.badge.own {
  background: #eff6ff;
  color: #1d4ed8;
}

.badge.danger {
  background: #fef2f2;
  color: #b91c1c;
}

.card-sub {
  color: #6b7280;
  font-size: 13px;
  margin-top: 4px;
}

.card-meta {
  color: #9ca3af;
  font-size: 12px;
  margin-top: 4px;
}

.switch {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: #4b5563;
  white-space: nowrap;
}

.script-warning {
  margin: 10px 0 0;
  padding: 8px 10px;
  background: #fef2f2;
  color: #b91c1c;
  border-radius: 8px;
  font-size: 12px;
}

.fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
  margin-top: 14px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  color: #4b5563;
  margin-top: 12px;
}

.fields .field {
  margin-top: 0;
}

.input {
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  padding: 7px 10px;
  font-size: 13px;
}

.mono {
  font-family: ui-monospace, monospace;
}

.field small {
  color: #9ca3af;
  font-size: 11px;
}

.code {
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  padding: 10px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  line-height: 1.5;
  tab-size: 4;
  white-space: pre;
  overflow-x: auto;
}

.editor-foot,
.card-foot {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 14px;
  flex-wrap: wrap;
}

.effective {
  font-size: 12px;
  color: #6b7280;
  margin-right: auto;
}

.btn-save,
.btn-primary {
  border: none;
  background: #111827;
  color: #fff;
  border-radius: 10px;
  padding: 8px 16px;
  font-size: 13px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.btn-danger {
  border: none;
  background: #fef2f2;
  color: #b91c1c;
  border-radius: 10px;
  padding: 8px 16px;
  font-size: 13px;
  cursor: pointer;
}

.btn-link {
  border: none;
  background: none;
  color: #2563eb;
  font-size: 13px;
  cursor: pointer;
}

.source {
  margin-top: 12px;
  border-top: 1px solid #eef0f4;
  padding-top: 12px;
}

.source h4 {
  font-size: 12px;
  color: #6b7280;
  margin: 8px 0 4px;
}

.source pre {
  background: #0f172a;
  color: #e2e8f0;
  padding: 12px;
  border-radius: 10px;
  font-size: 12px;
  overflow-x: auto;
  max-height: 320px;
}

.hint {
  font-size: 12px;
  color: #6b7280;
  background: #f9fafb;
  padding: 8px 10px;
  border-radius: 8px;
}
</style>
