<template>
  <!--
    Прелоадер списка. Повторяет форму строки заказа — иконка слева, две-три
    полосы текста, действие справа, — поэтому появление настоящих данных не
    сдвигает вёрстку и не даёт экрану «прыгнуть».
  -->
  <div class="skeleton-list" role="status" :aria-label="label">
    <div v-for="n in rows" :key="n" class="skeleton-row">
      <div v-if="icon" class="sk-icon sk-shimmer"></div>
      <div class="sk-text">
        <div class="sk-line sk-shimmer" style="width: 38%; height: 12px;"></div>
        <div class="sk-line sk-shimmer" style="width: 72%;"></div>
        <div v-if="lines > 2" class="sk-line sk-shimmer" style="width: 54%; height: 10px;"></div>
      </div>
      <div v-if="action" class="sk-action sk-shimmer"></div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent } from 'vue'

export default defineComponent({
  name: 'SkeletonList',
  props: {
    /** Сколько строк-заглушек показать. */
    rows: { type: Number, default: 3 },
    /** Полос текста в строке (3 — когда у элемента есть подпись с адресом). */
    lines: { type: Number, default: 2 },
    icon: { type: Boolean, default: true },
    action: { type: Boolean, default: false },
    label: { type: String, default: 'Загрузка' },
  },
})
</script>

<style scoped>
.skeleton-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.skeleton-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  background: #fff;
  border: 1px solid rgba(0, 0, 0, 0.05);
  border-radius: 14px;
}

.sk-icon {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  flex-shrink: 0;
}

.sk-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.sk-line {
  height: 11px;
  border-radius: 6px;
}

.sk-action {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  flex-shrink: 0;
}

/*
  Пульсация, а не бегущий блик: она читается как «идёт загрузка» и на медленном
  устройстве, где длинная анимация градиента заметно дороже.
*/
.sk-shimmer {
  background: linear-gradient(90deg, #eef1f6 25%, #e2e7ef 37%, #eef1f6 63%);
  background-size: 400% 100%;
  animation: sk-slide 1.4s ease-in-out infinite;
}

@keyframes sk-slide {
  0% {
    background-position: 100% 50%;
  }
  100% {
    background-position: 0 50%;
  }
}

@media (prefers-reduced-motion: reduce) {
  .sk-shimmer {
    animation: none;
    background: #eef1f6;
  }
}
</style>
