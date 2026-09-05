<template>
  <!--
    Тихая догрузка поверх уже показанных данных. Значок намеренно мелкий и не
    занимает места в потоке: он сообщает, что список сейчас уточнится, но не
    заставляет ждать и не перерисовывает то, что пользователь уже читает.
  -->
  <span v-if="active" class="refreshing-badge" :title="title">
    <span class="rb-spinner"></span>
    <span v-if="withLabel" class="rb-label">{{ title }}</span>
  </span>
</template>

<script lang="ts">
import { defineComponent } from 'vue'

export default defineComponent({
  name: 'RefreshingBadge',
  props: {
    active: { type: Boolean, default: false },
    withLabel: { type: Boolean, default: false },
    title: { type: String, default: 'Обновление…' },
  },
})
</script>

<style scoped>
.refreshing-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  vertical-align: middle;
  margin-left: 8px;
  color: var(--text-muted, #64748b);
  font-size: 11px;
  font-weight: 500;
}

.rb-spinner {
  width: 12px;
  height: 12px;
  border: 2px solid rgba(100, 116, 139, 0.25);
  border-top-color: var(--text-muted, #64748b);
  border-radius: 50%;
  animation: rb-spin 0.7s linear infinite;
}

@keyframes rb-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (prefers-reduced-motion: reduce) {
  .rb-spinner {
    animation-duration: 2s;
  }
}
</style>
