<template>
  <va-modal
    v-model="show"
    hide-default-actions
    max-width="500px"
    fixed-layout
    class="image-preview-modal-wrapper"
  >
    <div class="text-center p-3">
      <img
        :src="imageUrl"
        class="img-preview-content rounded shadow-lg"
        alt="preview"
        @error="$emit('error')"
      />
      <div class="mt-3 text-right">
        <va-button color="secondary" @click="show = false">
          {{ $t('common.close') }}
        </va-button>
      </div>
    </div>
  </va-modal>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue'

export default defineComponent({
  name: 'ImagePreviewModal',
  props: {
    modelValue: { type: Boolean, required: true },
    imageUrl: { type: String, default: '' },
  },
  emits: ['update:modelValue', 'error'],
  setup(props, { emit }) {
    const show = computed({
      get: () => props.modelValue,
      set: (val) => emit('update:modelValue', val),
    })

    return { show }
  },
})
</script>

<style scoped>
.img-preview-content {
  max-width: 100%;
  max-height: 70vh;
  object-fit: contain;
  margin: 0 auto;
}
</style>

<style>
/* Global override to ensure image preview modal and backdrop always sit above chat panel (z-index: 1000) and toast (z-index: 1050) */
.image-preview-modal-wrapper,
.image-preview-modal-wrapper .va-modal,
.image-preview-modal-wrapper .va-modal__overlay,
.image-preview-modal-wrapper .va-modal__container,
.image-preview-modal-wrapper .va-modal-dialog-slot {
  z-index: 99999 !important;
}

body .va-modal-container.image-preview-modal-wrapper,
body .va-modal-overlay.image-preview-modal-wrapper {
  z-index: 99999 !important;
}
</style>
