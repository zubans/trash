<template>
  <div class="p-2">
    <va-select
      v-if="!isEditing"
      v-model="form.node_type"
      :options="nodeTypeOptions"
      :label="$t('admin.nodeType')"
      text-by="label"
      value-by="value"
      track-by="value"
      class="mb-3"
    />

    <va-input
      v-if="!isEditing"
      v-model="form.code"
      :label="$t('admin.code')"
      class="mb-3"
    />

    <va-select
      v-model="form.parent_id"
      :options="parentOptions"
      :label="$t('admin.parent')"
      text-by="label"
      value-by="value"
      track-by="value"
      clearable
      class="mb-3"
    />

    <va-input
      v-model="form.name_ru"
      :label="$t('admin.nameRu')"
      class="mb-3"
      required
    />

    <va-input
      v-model="form.name_en"
      :label="$t('admin.nameEn')"
      class="mb-3"
    />

    <va-input
      v-model="form.description_ru"
      :label="$t('admin.descriptionRu')"
      type="textarea"
      class="mb-3"
    />

    <va-input
      v-model="form.description_en"
      :label="$t('admin.descriptionEn')"
      type="textarea"
      class="mb-3"
    />

    <va-input
      v-if="form.node_type === 'VARIANT'"
      v-model.number="form.base_price"
      :label="$t('admin.basePrice')"
      type="number"
      class="mb-3"
    />

    <div class="d-flex gap-3 mb-3">
      <va-checkbox v-model="form.is_auction" :label="$t('admin.isAuction')" />
      <va-checkbox v-model="form.is_active" :label="$t('admin.isActive')" />
    </div>

    <va-input
      v-model.number="form.sort_order"
      :label="$t('admin.sortOrder')"
      type="number"
      class="mb-3"
    />

    <div class="d-flex gap-2 justify-content-end">
      <va-button color="secondary" outline @click="$emit('cancel')">
        {{ $t('common.cancel') }}
      </va-button>
      <va-button color="primary" @click="submit">
        {{ $t('common.save') }}
      </va-button>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
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
    const { t } = useI18n()
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
          base_price: props.node.base_price,
          is_auction: props.node.is_auction,
          is_active: props.node.is_active,
          sort_order: props.node.sort_order,
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
        sort_order: 0,
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

    const nodeTypeOptions = [
      { label: t('admin.category'), value: 'CATEGORY' },
      { label: t('admin.variant'), value: 'VARIANT' },
    ]

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
      nodeTypeOptions,
      submit,
    }
  },
})
</script>
