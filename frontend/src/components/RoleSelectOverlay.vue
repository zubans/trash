<template>
  <div class="role-select-overlay">
    <div class="role-select-card">
      <div class="rs-head">
        <h2 class="rs-title">Как продолжить?</h2>
        <p class="rs-sub">У вашего аккаунта несколько ролей. Выберите, кем работать сейчас — сменить роль можно в любой момент в меню.</p>
      </div>

      <div class="rs-options">
        <button
          v-for="opt in options"
          :key="opt.value"
          type="button"
          class="rs-option"
          @click="$emit('select', opt.value)"
        >
          <div class="rs-icon" :style="{ background: opt.color }">
            <i :class="opt.icon"></i>
          </div>
          <div class="rs-text">
            <span class="rs-name">{{ opt.label }}</span>
            <span class="rs-desc">{{ opt.desc }}</span>
          </div>
          <i class="ph-bold ph-caret-right rs-arrow"></i>
        </button>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed, type PropType } from 'vue'

const META: Record<string, { label: string; desc: string; icon: string; color: string }> = {
  CUSTOMER: {
    label: 'Заказчик',
    desc: 'Создавать заказы и следить за их выполнением',
    icon: 'ph-fill ph-user',
    color: '#6366f1',
  },
  EXECUTOR: {
    label: 'Исполнитель',
    desc: 'Брать заказы поблизости и выполнять их',
    icon: 'ph-fill ph-briefcase',
    color: '#10b981',
  },
  ADMIN: {
    label: 'Администратор',
    desc: 'Управление платформой',
    icon: 'ph-fill ph-shield-check',
    color: '#0ea5e9',
  },
}

export default defineComponent({
  name: 'RoleSelectOverlay',
  props: {
    roles: { type: Array as PropType<string[]>, required: true },
  },
  emits: ['select'],
  setup(props) {
    const options = computed(() =>
      props.roles
        .filter((r) => META[r])
        .map((r) => ({ value: r, ...META[r] })),
    )
    return { options }
  },
})
</script>

<style scoped>
.role-select-overlay {
  position: fixed;
  inset: 0;
  z-index: 4000;
  background: rgba(15, 23, 42, 0.55);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  font-family: 'Outfit', sans-serif;
}

.role-select-card {
  background: #ffffff;
  border-radius: 24px;
  width: 100%;
  max-width: 420px;
  padding: 28px 22px;
  box-shadow: 0 24px 60px -12px rgba(0, 0, 0, 0.35);
  animation: rs-in 0.25s ease-out;
}
@keyframes rs-in {
  from { opacity: 0; transform: translateY(14px); }
  to { opacity: 1; transform: translateY(0); }
}

.rs-head {
  margin-bottom: 20px;
  text-align: center;
}
.rs-title {
  font-size: 22px;
  font-weight: 700;
  color: #0f172a;
  margin: 0 0 6px;
}
.rs-sub {
  font-size: 13px;
  color: #64748b;
  margin: 0;
  line-height: 1.45;
}

.rs-options {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.rs-option {
  display: flex;
  align-items: center;
  gap: 14px;
  width: 100%;
  text-align: left;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 16px;
  padding: 14px 16px;
  cursor: pointer;
  transition: all 0.15s ease;
}
.rs-option:hover {
  background: #eef2ff;
  border-color: #c7d2fe;
  transform: translateY(-1px);
}
.rs-icon {
  width: 46px;
  height: 46px;
  border-radius: 13px;
  color: #fff;
  font-size: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.rs-text {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}
.rs-name {
  font-size: 16px;
  font-weight: 700;
  color: #0f172a;
}
.rs-desc {
  font-size: 12px;
  color: #64748b;
  line-height: 1.35;
}
.rs-arrow {
  color: #94a3b8;
  font-size: 16px;
  flex-shrink: 0;
}
</style>
