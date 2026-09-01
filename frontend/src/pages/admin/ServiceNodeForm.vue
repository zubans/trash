<template>
  <div class="catalog-form-container">
    <!-- Header -->
    <div class="form-header">
      <div class="header-icon-wrap">
        <i class="ph-fill ph-folders header-icon"></i>
      </div>
      <div class="header-titles">
        <h3 class="form-title">
          {{ isEditing ? 'Редактирование элемента каталога' : 'Новый элемент каталога' }}
        </h3>
        <p class="form-subtitle">
          {{ isEditing ? 'Изменение параметров категории или варианта услуги' : 'Добавление категории или варианта услуги' }}
        </p>
      </div>
      <button type="button" class="btn-close-modal" title="Закрыть" @click="$emit('cancel')">
        <i class="ph ph-x"></i>
      </button>
    </div>

    <!-- Form Body -->
    <div class="form-body">
      <!-- Row 1: Node Type & System Code -->
      <div class="form-grid grid-2 mb-4">
        <div class="form-group">
          <label class="form-label">ТИП УЗЛА <span class="req">*</span></label>
          <div class="select-wrapper">
            <select v-model="form.node_type" class="form-select" :disabled="isEditing">
              <option value="CATEGORY">Категория (Category)</option>
              <option value="VARIANT">Вариант услуги (Variant)</option>
            </select>
            <i class="ph-bold ph-caret-down select-arrow"></i>
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">СИСТЕМНЫЙ КОД (CODE) <span class="req">*</span></label>
          <input
            v-model="form.code"
            type="text"
            class="form-input code-font"
            placeholder="напр. dog_walk_morning"
            :disabled="isEditing"
            required
          />
        </div>
      </div>

      <!-- Row 2: Parent Category -->
      <div class="form-group mb-4">
        <label class="form-label">РОДИТЕЛЬСКАЯ КАТЕГОРИЯ</label>
        <div class="select-wrapper">
          <select v-model="form.parent_id" class="form-select">
            <option :value="undefined">-- Корневой каталог --</option>
            <option v-for="opt in parentOptions" :key="opt.value" :value="opt.value">
              {{ opt.label }}
            </option>
          </select>
          <i class="ph-bold ph-caret-down select-arrow"></i>
        </div>
      </div>

      <div class="form-divider mb-4"></div>

      <!-- Row 3: Names (RU / EN) -->
      <div class="form-grid grid-2 mb-4">
        <div class="form-group">
          <label class="form-label">НАЗВАНИЕ (РУССКИЙ) <span class="req">*</span></label>
          <div class="input-badge-wrap">
            <span class="lang-badge">RU</span>
            <input
              v-model="form.name_ru"
              type="text"
              class="form-input with-badge"
              placeholder="Утренний выгул"
              required
            />
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">НАЗВАНИЕ (ENGLISH)</label>
          <div class="input-badge-wrap">
            <span class="lang-badge">EN</span>
            <input
              v-model="form.name_en"
              type="text"
              class="form-input with-badge"
              placeholder="Morning walk"
            />
          </div>
        </div>
      </div>

      <!-- Row 4: Descriptions (RU / EN) -->
      <div class="form-grid grid-2 mb-4">
        <div class="form-group">
          <label class="form-label">ОПИСАНИЕ (РУССКИЙ)</label>
          <div class="input-badge-wrap">
            <span class="lang-badge">RU</span>
            <input
              v-model="form.description_ru"
              type="text"
              class="form-input with-badge"
              placeholder="Описание услуги..."
            />
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">ОПИСАНИЕ (ENGLISH)</label>
          <div class="input-badge-wrap">
            <span class="lang-badge">EN</span>
            <input
              v-model="form.description_en"
              type="text"
              class="form-input with-badge"
              placeholder="Service description..."
            />
          </div>
        </div>
      </div>

      <!-- Row 5: Sort Order, Base Price & Min Age -->
      <div class="form-grid grid-3 mb-4">
        <div class="form-group">
          <label class="form-label">ПОРЯДОК (СОРТИРОВКА)</label>
          <input
            v-model.number="form.sort_order"
            type="number"
            class="form-input"
            placeholder="1"
          />
        </div>

        <div v-if="form.node_type === 'VARIANT'" class="form-group">
          <label class="form-label">БАЗОВАЯ ЦЕНА (РУБ)</label>
          <input
            v-model.number="form.base_price"
            type="number"
            class="form-input"
            placeholder="0"
          />
        </div>

        <div class="form-group">
          <label class="form-label">ВОЗРАСТНОЙ ЦЕНЗ (ЛЕТ)</label>
          <input
            v-model.number="form.min_age"
            type="number"
            class="form-input"
            placeholder="0 (без ограничений)"
            min="0"
          />
        </div>
      </div>

      <!-- Row 6: Toggles Container -->
      <div class="toggles-card">
        <label class="toggle-item">
          <input v-model="form.is_active" type="checkbox" class="toggle-checkbox" />
          <span class="toggle-switch"></span>
          <span class="toggle-label">Активно в приложении</span>
        </label>

        <label class="toggle-item">
          <input v-model="form.is_auction" type="checkbox" class="toggle-checkbox" />
          <span class="toggle-switch"></span>
          <span class="toggle-label">Режим аукциона</span>
        </label>

        <label class="toggle-item">
          <input v-model="form.requires_verification" type="checkbox" class="toggle-checkbox" />
          <span class="toggle-switch"></span>
          <span class="toggle-label">Только верифицированные</span>
        </label>

        <label class="toggle-item">
          <input v-model="form.moderator_only" type="checkbox" class="toggle-checkbox" />
          <span class="toggle-switch"></span>
          <span class="toggle-label">Только для модераторов</span>
        </label>
      </div>

      <!-- Row 7: Special service. The flags above cover the rules that are the
           same for every service; a script carries the ones that are not. -->
      <div class="form-divider mt-4 mb-4"></div>

      <label class="toggle-item special-toggle">
        <input v-model="form.is_special" type="checkbox" class="toggle-checkbox" @change="onSpecialToggled" />
        <span class="toggle-switch"></span>
        <span class="toggle-label">
          Спец-услуга (правила задаются скриптом)
        </span>
      </label>

      <div v-if="form.is_special" class="special-section">
        <div class="special-head">
          <div class="select-wrapper template-select">
            <select v-model="template" class="form-select" @change="applyTemplate">
              <option value="">Шаблон: пустой</option>
              <option v-for="b in behaviors" :key="b.code" :value="b.code">
                Шаблон: {{ b.name }}
              </option>
            </select>
            <i class="ph-bold ph-caret-down select-arrow"></i>
          </div>
          <router-link
            class="help-link"
            :to="{ name: 'admin-service-scripts-help' }"
            target="_blank"
          >
            <i class="ph-bold ph-question"></i> Как писать скрипты
          </router-link>
        </div>

        <p v-if="fromLibrary" class="behavior-hint">
          Скрипт «{{ selectedBehavior?.name }}» поставляется с приложением.
          Сохранение услуги создаст её собственную копию, и дальше она живёт
          отдельно от поставляемой.
        </p>

        <div class="form-group mb-4">
          <label class="form-label">КОНСТАНТЫ И ПЕРЕМЕННЫЕ</label>
          <textarea
            v-model="form.behavior_constants"
            class="form-input code-font script-input"
            rows="10"
            spellcheck="false"
            placeholder="REWARD = 100"
          ></textarea>
          <p class="behavior-hint">
            Суммы, роли, тексты сообщений — всё, что меняют, не читая логику.
            Выполняется перед скриптом, поэтому в нём доступно по имени.
          </p>
        </div>

        <div class="form-group">
          <label class="form-label">СКРИПТ УСЛУГИ</label>
          <textarea
            v-model="form.behavior_source"
            class="form-input code-font script-input"
            rows="18"
            spellcheck="false"
            placeholder="MANIFEST = { ... }"
          ></textarea>
          <p v-if="scriptError" class="behavior-error">{{ scriptError }}</p>
          <p v-else class="behavior-hint">
            <code>MANIFEST</code> и хуки <code>visible</code>,
            <code>can_order</code>, <code>can_view_or_take</code>,
            <code>price</code>, <code>on_event</code>. Скрипт компилируется при
            сохранении: с ошибкой услуга не сохранится.
          </p>
        </div>
      </div>
    </div>

    <!-- Footer -->
    <p v-if="saveError" class="save-error">{{ saveError }}</p>
    <div class="form-footer">
      <button type="button" class="btn-cancel" @click="$emit('cancel')">
        Отмена
      </button>
      <button type="button" class="btn-submit" @click="submit">
        <i class="ph-bold ph-check me-1"></i> Сохранить изменения
      </button>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch, onMounted, type PropType } from 'vue'
import type { ServiceNode } from '../../api/services'
import { getServiceBehaviors, type ServiceBehavior } from '../../api/admin-services'

export default defineComponent({
  name: 'ServiceNodeForm',
  props: {
    node: {
      type: Object as PropType<ServiceNode | null>,
      default: null,
    },
    parentOptions: {
      type: Array as PropType<Array<{ label: string; value: string }>>,
      default: () => [],
    },
    initialParentId: {
      type: String as PropType<string | null>,
      default: null,
    },
    // What the server said when it refused the last save — a script that does
    // not compile, most often. Shown in the footer, where the admin is looking.
    saveError: {
      type: String as PropType<string>,
      default: '',
    },
  },
  emits: ['save', 'cancel'],
  setup(props, { emit }) {
    const isEditing = computed(() => props.node !== null)

    // The behaviour list comes from the server, which reads it from the scripts
    // themselves: a behaviour deployed today is selectable here today, with no
    // change to this form.
    const behaviors = ref<ServiceBehavior[]>([])
    const scriptError = ref('')
    const template = ref('')

    // The two fields of an empty special service. A blank editor is a worse
    // starting point than a script that already compiles and does nothing
    // surprising.
    const EMPTY_CONSTANTS = `# Константы и переменные услуги.
# Суммы, роли, тексты сообщений — всё, что меняют, не читая логику.

REWARD = 0
MSG_UNAVAILABLE = "услуга сейчас недоступна"
`
    const EMPTY_SCRIPT = `# Правила услуги. Определяйте только нужные хуки:
# отсутствующий хук означает «нет мнения» — работает обычное правило платформы.

MANIFEST = {
    "name": "Новая спец-услуга",
    "description": "",
    "once_per_user": False,
    "events": [],
}

def visible(f):
    # Кому услуга видна в каталоге.
    return f.user != None

def can_order(f):
    # None — можно заказать; строка — отказ с этим текстом.
    if f.user == None:
        return MSG_UNAVAILABLE
    return None

# def price(f):
#     return 0

# def can_view_or_take(f):
#     if not has_role(f.viewer, "MODERATOR"):
#         return "заказ выполняют только модераторы"
#     return None

# def on_event(f):
#     return []
`

    const buildForm = () => {
      if (props.node) {
        return {
          parent_id: props.node.parent_id || undefined,
          code: props.node.code,
          name_ru: props.node.name.ru || '',
          name_en: props.node.name.en || '',
          description_ru: props.node.description?.ru || '',
          description_en: props.node.description?.en || '',
          node_type: props.node.node_type,
          base_price: props.node.base_price || 0,
          is_auction: props.node.is_auction || false,
          is_active: props.node.is_active ?? true,
          sort_order: props.node.sort_order || 1,
          requires_verification: props.node.requires_verification || false,
          moderator_only: props.node.moderator_only || false,
          min_age: props.node.min_age || 0,
          behavior_code: props.node.behavior_code || '',
          behavior_config: props.node.behavior_config || {},
          // A node is special when it runs a script: its own, or a library one
          // it names. Both are shown in the editor; saving a library one makes
          // the copy its own.
          is_special: Boolean(props.node.behavior_source || props.node.behavior_code),
          behavior_constants: props.node.behavior_constants || '',
          behavior_source: props.node.behavior_source || '',
        }
      }
      return {
        parent_id: props.initialParentId || undefined,
        code: '',
        name_ru: '',
        name_en: '',
        description_ru: '',
        description_en: '',
        node_type: 'CATEGORY',
        base_price: 0,
        is_auction: false,
        is_active: true,
        sort_order: 1,
        requires_verification: false,
        moderator_only: false,
        min_age: 0,
        behavior_code: '',
        behavior_config: {} as Record<string, unknown>,
        is_special: false,
        behavior_constants: '',
        behavior_source: '',
      }
    }

    const form = ref(buildForm())

    const selectedBehavior = computed(() =>
      behaviors.value.find((b) => b.code === form.value.behavior_code) || null
    )

    // True while the node still runs the library script rather than a copy of
    // its own — the state the editor warns about, because saving forks it.
    const fromLibrary = computed(
      () => Boolean(form.value.behavior_code) && !props.node?.behavior_source
    )

    // Ticking the flag on an empty node fills both fields, so the admin starts
    // from something that compiles instead of a blank page.
    const onSpecialToggled = () => {
      scriptError.value = ''
      if (!form.value.is_special) {
        return
      }
      if (!form.value.behavior_source) {
        applyTemplate()
      }
    }

    // Loading a template replaces both fields. Only ever done on request: it
    // would otherwise overwrite an edit in progress.
    const applyTemplate = () => {
      scriptError.value = ''
      const chosen = behaviors.value.find((b) => b.code === template.value)
      if (!chosen) {
        form.value.behavior_constants = EMPTY_CONSTANTS
        form.value.behavior_source = EMPTY_SCRIPT
        return
      }
      form.value.behavior_constants = chosen.constants_source || ''
      form.value.behavior_source = chosen.source || ''
    }

    // A node running a library script shows that script, so the editor always
    // displays the rules the service actually runs.
    const showLibrarySource = () => {
      if (!form.value.behavior_code || form.value.behavior_source) {
        return
      }
      const library = behaviors.value.find((b) => b.code === form.value.behavior_code)
      if (library) {
        form.value.behavior_constants = library.constants_source || ''
        form.value.behavior_source = library.source || ''
      }
    }

    onMounted(async () => {
      try {
        behaviors.value = await getServiceBehaviors()
        showLibrarySource()
      } catch {
        // An admin panel that cannot list the library must still be able to edit
        // the rest of a service; only the templates are missing.
        behaviors.value = []
      }
    })

    watch(
      () => props.node,
      () => {
        form.value = buildForm()
      },
      { immediate: true }
    )

    const submit = () => {
      scriptError.value = ''
      if (form.value.is_special && !form.value.behavior_source.trim()) {
        scriptError.value = 'Спец-услуге нужен скрипт: выберите шаблон или напишите свой'
        return
      }

      const payload: any = {
        parent_id: form.value.parent_id || undefined,
        name: {
          ru: form.value.name_ru,
          ...(form.value.name_en ? { en: form.value.name_en } : {}),
        },
        description: {
          ru: form.value.description_ru,
          ...(form.value.description_en ? { en: form.value.description_en } : {}),
        },
        base_price: form.value.node_type === 'VARIANT' ? form.value.base_price : undefined,
        is_auction: form.value.is_auction,
        is_active: form.value.is_active,
        sort_order: form.value.sort_order,
        requires_verification: form.value.requires_verification,
        moderator_only: form.value.moderator_only,
        min_age: form.value.min_age || 0,
        // The code records which library script this started from; the text
        // below is what the service actually runs.
        behavior_code: form.value.is_special ? form.value.behavior_code || '' : '',
        behavior_config: form.value.is_special ? form.value.behavior_config : {},
        behavior_constants: form.value.is_special ? form.value.behavior_constants : '',
        behavior_source: form.value.is_special ? form.value.behavior_source : '',
      }

      if (!isEditing.value) {
        payload.code = form.value.code
        payload.node_type = form.value.node_type
      }

      if (!payload.description.ru && !payload.description.en) {
        delete payload.description
      }

      emit('save', payload)
    }

    return {
      form,
      isEditing,
      behaviors,
      selectedBehavior,
      fromLibrary,
      template,
      applyTemplate,
      onSpecialToggled,
      scriptError,
      submit,
    }
  },
})
</script>

<style scoped>
.catalog-form-container {
  background: #ffffff;
  border-radius: 24px;
  width: 100%;
  max-width: 820px;
  box-shadow: 0 20px 40px -15px rgba(15, 23, 42, 0.15);
  overflow: hidden;
  font-family: inherit;
  margin: 0 auto;
}

/* Header */
.form-header {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 24px 28px;
  background: #ffffff;
  border-bottom: 1px solid #f1f5f9;
  position: relative;
}

.header-icon-wrap {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: rgba(99, 102, 241, 0.1);
  color: #6366f1;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 22px;
  flex-shrink: 0;
}

.header-titles {
  flex: 1;
}

.form-title {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  color: #0f172a;
  letter-spacing: -0.3px;
}

.form-subtitle {
  margin: 2px 0 0 0;
  font-size: 13px;
  color: #64748b;
}

.btn-close-modal {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: none;
  background: #f8fafc;
  color: #64748b;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-close-modal:hover {
  background: #f1f5f9;
  color: #0f172a;
}

/* Body */
.form-body {
  padding: 24px 28px;
  background: #fafbfd;
}

.form-grid {
  display: grid;
  gap: 18px;
}

.grid-2 {
  grid-template-columns: 1fr 1fr;
}

.grid-3 {
  grid-template-columns: 1fr 1fr 1fr;
}

.grid-bottom {
  display: flex;
  gap: 18px;
  align-items: flex-end;
}

.flex-1 { flex: 1; }
.flex-2 { flex: 2; }

.form-group {
  display: flex;
  flex-direction: column;
}

.form-label {
  font-size: 11px;
  font-weight: 700;
  color: #475569;
  letter-spacing: 0.5px;
  text-transform: uppercase;
  margin-bottom: 6px;
}

.req {
  color: #ef4444;
}

.form-input {
  height: 48px;
  border-radius: 12px;
  border: 1px solid #e2e8f0;
  font-size: 14px;
  color: #0f172a;
  background: #ffffff;
  padding: 0 16px;
  font-family: inherit;
  transition: all 0.2s ease;
  width: 100%;
}

.form-input:focus {
  border-color: #6366f1;
  box-shadow: 0 0 0 3px rgba(99, 102, 241, 0.15);
  outline: none;
}

.form-input:disabled {
  background: #f1f5f9;
  color: #94a3b8;
  cursor: not-allowed;
}

.code-font {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.select-wrapper {
  position: relative;
  width: 100%;
}

.form-select {
  height: 48px;
  border-radius: 12px;
  border: 1px solid #e2e8f0;
  font-size: 14px;
  color: #0f172a;
  background: #ffffff;
  padding: 0 40px 0 16px;
  font-family: inherit;
  appearance: none;
  width: 100%;
  cursor: pointer;
  transition: all 0.2s ease;
}

.form-select:focus {
  border-color: #6366f1;
  box-shadow: 0 0 0 3px rgba(99, 102, 241, 0.15);
  outline: none;
}

.select-arrow {
  position: absolute;
  right: 16px;
  top: 50%;
  transform: translateY(-50%);
  color: #94a3b8;
  pointer-events: none;
  font-size: 14px;
}

.input-badge-wrap {
  position: relative;
  width: 100%;
}

.lang-badge {
  position: absolute;
  left: 12px;
  top: 50%;
  transform: translateY(-50%);
  font-size: 11px;
  font-weight: 700;
  color: #64748b;
  background: #f1f5f9;
  padding: 4px 7px;
  border-radius: 6px;
  letter-spacing: 0.5px;
  pointer-events: none;
}

.form-input.with-badge {
  padding-left: 48px;
}

.form-divider {
  height: 1px;
  border-top: 1px dashed #e2e8f0;
}

.behavior-hint {
  margin: 8px 0 0;
  font-size: 12px;
  color: #64748b;
  line-height: 1.5;
}

.behavior-error {
  margin: 8px 0 0;
  font-size: 12px;
  color: #dc2626;
}

.save-error {
  margin: 0;
  padding: 12px 28px;
  background: #fef2f2;
  color: #b91c1c;
  font-size: 13px;
  line-height: 1.5;
  white-space: pre-wrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  border-top: 1px solid #fecaca;
}

.special-toggle {
  margin-bottom: 16px;
}

.special-section {
  border: 1px solid #e2e8f0;
  border-radius: 16px;
  padding: 18px 20px;
  background: #fbfcfe;
}

.special-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}

.template-select {
  max-width: 320px;
}

.help-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  color: #6366f1;
  text-decoration: none;
  white-space: nowrap;
}

.help-link:hover {
  text-decoration: underline;
}

.script-input {
  height: auto;
  padding: 12px 16px;
  line-height: 1.55;
  font-size: 13px;
  resize: vertical;
  white-space: pre;
  overflow-wrap: normal;
  overflow-x: auto;
}

/* Toggles Card */
.toggles-card {
  background: #ffffff;
  border: 1px solid #e2e8f0;
  border-radius: 16px;
  padding: 10px 20px;
  display: flex;
  align-items: center;
  gap: 24px;
  height: 48px;
}

.toggle-item {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  user-select: none;
}

.toggle-checkbox {
  display: none;
}

.toggle-switch {
  width: 44px;
  height: 24px;
  border-radius: 12px;
  background: #cbd5e1;
  position: relative;
  transition: background 0.2s ease;
  flex-shrink: 0;
}

.toggle-switch::after {
  content: '';
  position: absolute;
  top: 3px;
  left: 3px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: #ffffff;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.15);
  transition: transform 0.2s ease;
}

.toggle-checkbox:checked + .toggle-switch {
  background: #10b981;
}

.toggle-checkbox:checked + .toggle-switch::after {
  transform: translateX(20px);
}

.toggle-label {
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
}

/* Footer */
.form-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  padding: 20px 28px;
  background: #ffffff;
  border-top: 1px solid #f1f5f9;
}

.btn-cancel {
  background: #ffffff;
  color: #475569;
  font-size: 14px;
  font-weight: 600;
  padding: 12px 24px;
  border-radius: 12px;
  border: 1px solid #e2e8f0;
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-cancel:hover {
  background: #f8fafc;
  color: #0f172a;
}

.btn-submit {
  background: #5c60f5;
  color: #ffffff;
  font-size: 14px;
  font-weight: 600;
  padding: 12px 24px;
  border-radius: 12px;
  border: none;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  box-shadow: 0 4px 12px rgba(92, 96, 245, 0.25);
  transition: all 0.2s ease;
}

.btn-submit:hover {
  background: #4f52e6;
  box-shadow: 0 6px 16px rgba(92, 96, 245, 0.35);
}

@media (max-width: 640px) {
  .grid-2 { grid-template-columns: 1fr; }
  .grid-bottom { flex-direction: column; align-items: stretch; }
  .toggles-card { flex-direction: column; align-items: flex-start; height: auto; padding: 14px; }
}
</style>
