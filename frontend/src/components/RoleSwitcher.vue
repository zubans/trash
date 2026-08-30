<template>
  <div v-if="showSwitcher" class="role-switcher">
    <i class="ph-bold ph-arrows-left-right rs-icon"></i>
    <select class="rs-select" :value="authStore.activeRole" @change="onChange">
      <option v-for="opt in options" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
    </select>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth-store'

const LABELS: Record<string, string> = {
  CUSTOMER: 'Заказчик',
  EXECUTOR: 'Исполнитель',
  ADMIN: 'Администратор',
}
const HOME: Record<string, string> = {
  CUSTOMER: '/customer',
  EXECUTOR: '/executor',
  ADMIN: '/admin',
}

// A compact switcher for users who hold more than one dashboard role. It changes
// the active role and navigates to that role's dashboard. Hidden for single-role
// users (the common case). MODERATOR shares the executor dashboard and is not a
// separate switch target.
export default defineComponent({
  name: 'RoleSwitcher',
  setup() {
    const authStore = useAuthStore()
    const router = useRouter()

    const options = computed(() =>
      authStore.switchableRoles.map((r) => ({ value: r, label: LABELS[r] || r }))
    )
    const showSwitcher = computed(() => options.value.length > 1)

    const onChange = (e: Event) => {
      const role = (e.target as HTMLSelectElement).value
      authStore.setActiveRole(role)
      const home = HOME[role]
      if (home) router.push(home)
    }

    return { authStore, options, showSwitcher, onChange }
  },
})
</script>

<style scoped>
.role-switcher {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: #f1f5f9;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  padding: 6px 10px;
}
.rs-icon {
  color: #6366f1;
  font-size: 15px;
}
.rs-select {
  border: none;
  background: transparent;
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
  cursor: pointer;
  outline: none;
}
</style>
