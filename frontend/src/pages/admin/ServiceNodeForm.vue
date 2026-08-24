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

      <!-- Row 5: Sort Order, Base Price & Toggles -->
      <div class="form-grid grid-bottom">
        <div class="form-group flex-1">
          <label class="form-label">ПОРЯДОК (СОРТИРОВКА)</label>
          <input
            v-model.number="form.sort_order"
            type="number"
            class="form-input"
            placeholder="1"
          />
        </div>

        <div v-if="form.node_type === 'VARIANT'" class="form-group flex-1">
          <label class="form-label">БАЗОВАЯ ЦЕНА (РУБ)</label>
          <input
            v-model.number="form.base_price"
            type="number"
            class="form-input"
            placeholder="0"
          />
        </div>

        <!-- Toggles Container -->
        <div class="toggles-card flex-2">
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
        </div>
      </div>
    </div>

    <!-- Footer -->
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
import { defineComponent, ref, computed, watch, type PropType } from 'vue'
import type { ServiceNode } from '../../api/services'

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
  },
  emits: ['save', 'cancel'],
  setup(props, { emit }) {
    const isEditing = computed(() => props.node !== null)

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
      }
    }

    const form = ref(buildForm())

    watch(
      () => props.node,
      () => {
        form.value = buildForm()
      },
      { immediate: true }
    )

    const submit = () => {
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
