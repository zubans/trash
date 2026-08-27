<template>
  <div class="logo-container opt-1" :class="{ compact: compact }">
    <svg class="logo-svg" viewBox="0 0 40 40" fill="none">
      <path
        d="M 6 35 V 17 C 6 9, 15 9, 20 18 C 25 9, 32 9, 32 17 V 23"
        stroke="var(--brand-primary, #5c60f5)"
        stroke-width="7"
        stroke-linecap="round"
        stroke-linejoin="round"
      ></path>
      <circle cx="19" cy="27" r="4.5" fill="var(--success-main, #10b981)"></circle>
    </svg>
    <div v-if="!hideText" class="text-block">
      <div class="text-top">оя</div>
      <div class="text-bottom">услуга</div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent } from 'vue'

export default defineComponent({
  name: 'AppLogo',
  props: {
    hideText: {
      type: Boolean,
      default: false,
    },
    compact: {
      type: Boolean,
      default: false,
    },
  },
})
</script>

<style scoped>
.logo-container {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  user-select: none;
}

.logo-svg {
  width: 36px;
  height: 36px;
  flex-shrink: 0;
}

.logo-container.compact .logo-svg {
  width: 28px;
  height: 28px;
}

.text-block {
  /* Optical alignment of the wordmark against the SVG "М" stem.
     Grouped here so the offsets are tunable in one place. */
  --top-offset-x: -9px;
  --top-offset-y: 12px;
  --bottom-offset-x: -18px;
  --bottom-offset-y: -4px;

  display: flex;
  flex-direction: column;
  line-height: 1;
}

.text-top {
  font-size: 17px;
  font-weight: 800;
  color: #0f172a;
  letter-spacing: -0.5px;
  margin-top: var(--top-offset-y);
  margin-left: var(--top-offset-x);
}

.text-bottom {
  font-size: 18px;
  font-weight: 700;
  color: #5c60f5;
  letter-spacing: 0.3px;
  margin-top: var(--bottom-offset-y);
  margin-left: var(--bottom-offset-x);
}

.logo-container.compact .text-top {
  font-size: 14px;
}

.logo-container.compact .text-bottom {
  font-size: 11px;
}
</style>
