<template>
  <div class="hint-overlay" @click.self="$emit('cancel')">
    <div class="hint-card" role="dialog" aria-modal="true">
      <div class="hint-badge"><i class="ph-fill ph-hand-peace"></i></div>
      <h3 class="hint-title">Покажите в кадре: {{ gesture.title }}</h3>
      <p class="hint-text">{{ gesture.description }}</p>
      <img v-if="imageSrc" :src="imageSrc" alt="" class="hint-image" />

      <p v-if="selfie && !gesture.fits_in_selfie" class="hint-note">
        Этот жест в селфи не помещается — в селфи его показывать не нужно. Жест должен быть на снимке места заказа.
      </p>
      <p v-else-if="selfie" class="hint-note">
        Сделайте селфи с заказчиком и покажите жест в кадре.
      </p>
      <p v-else class="hint-note">
        Сфотографируйте место заказа так, чтобы рядом с объектом был виден жест.
      </p>

      <div class="hint-actions">
        <button type="button" class="hint-btn secondary" @click="$emit('cancel')">Отмена</button>
        <button type="button" class="hint-btn primary" @click="$emit('confirm')">
          <i class="ph-bold ph-camera me-1"></i> Снимаю
        </button>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, type PropType } from 'vue'
import { resolveFileUrl } from '../../services/api'

export interface Gesture {
  code: string
  number: number
  title: string
  description: string
  hint_image_url?: string
  fits_in_selfie: boolean
}

export default defineComponent({
  name: 'WatermarkHintPopup',
  props: {
    gesture: { type: Object as PropType<Gesture>, required: true },
    selfie: { type: Boolean, default: false },
  },
  emits: ['confirm', 'cancel'],
  setup(props) {
    const imageSrc = computed(() => {
      const url = props.gesture.hint_image_url
      if (!url) return ''
      return url.startsWith('http') ? url : resolveFileUrl(url)
    })
    return { imageSrc }
  },
})
</script>

<style scoped>
.hint-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  z-index: 1300;
}
.hint-card {
  background: #fff;
  border-radius: 20px;
  width: 100%;
  max-width: 400px;
  padding: 22px 20px 18px;
  text-align: center;
  box-shadow: 0 24px 48px -16px rgba(15, 23, 42, 0.35);
}
.hint-badge {
  width: 56px;
  height: 56px;
  margin: 0 auto 10px;
  border-radius: 16px;
  background: #eef2ff;
  color: #4f46e5;
  font-size: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.hint-title {
  margin: 0 0 8px;
  font-size: 18px;
  font-weight: 700;
  color: #0f172a;
}
.hint-text {
  margin: 0 0 12px;
  font-size: 14px;
  color: #334155;
  line-height: 1.45;
}
.hint-image {
  max-height: 160px;
  border-radius: 12px;
  margin-bottom: 12px;
}
.hint-note {
  margin: 0 0 16px;
  padding: 10px 12px;
  background: #f8fafc;
  border-radius: 10px;
  font-size: 13px;
  color: #475569;
  line-height: 1.4;
}
.hint-actions {
  display: flex;
  gap: 10px;
}
.hint-btn {
  flex: 1;
  height: 46px;
  border: none;
  border-radius: 12px;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  font-family: inherit;
}
.hint-btn.secondary {
  background: #f1f5f9;
  color: #475569;
}
.hint-btn.primary {
  background: #4f46e5;
  color: #fff;
}
</style>
