<template>
  <div class="roles-admin">
    <header class="page-head">
      <div>
        <h1>Роли и права</h1>
        <p class="page-sub">
          Роль — это набор разделов панели и то, что в них разрешено делать.
          Пользователь может носить несколько ролей сразу; тогда ему доступно
          объединение их прав. Администратор здесь особый: у него всегда есть всё,
          и снятая галочка не может запереть его снаружи панели.
        </p>
      </div>
      <button type="button" class="btn-refresh" :disabled="loading" @click="load">
        <i class="ph-bold ph-arrows-clockwise"></i>
      </button>
    </header>

    <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>
    <p v-if="successMsg" class="alert success">{{ successMsg }}</p>

    <section class="panel">
      <div class="panel-head">
        <h2>Справочник</h2>
        <button v-if="canCreate" type="button" class="btn-primary" @click="startCreate">
          <i class="ph-bold ph-plus"></i> Новая роль
        </button>
      </div>
      <table class="role-table">
        <thead>
          <tr>
            <th>Код</th>
            <th>Название</th>
            <th>Носителей</th>
            <th>Права</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="role in roles"
            :key="role.code"
            :class="{ selected: selected && selected.code === role.code }"
          >
            <td class="mono">
              {{ role.code }}
              <span v-if="role.is_system" class="badge muted" title="Системная роль: удалить нельзя">
                системная
              </span>
            </td>
            <td>
              <div>{{ role.name }}</div>
              <div v-if="role.description" class="muted-text">{{ role.description }}</div>
            </td>
            <td>
              <button type="button" class="btn-link" @click="openUsers(role)">
                {{ role.user_count }}
              </button>
            </td>
            <td>{{ permissionSummary(role) }}</td>
            <td class="actions">
              <button type="button" class="btn-link" @click="startEdit(role)">
                {{ canEdit && role.code !== 'ADMIN' ? 'права' : 'смотреть' }}
              </button>
              <button type="button" class="btn-link" @click="openUsers(role)">носители</button>
              <button
                v-if="canDelete && !role.is_system"
                type="button"
                class="btn-link danger"
                @click="askDelete(role)"
              >
                удалить
              </button>
            </td>
          </tr>
          <tr v-if="!roles.length && !loading">
            <td colspan="5" class="empty">Ролей пока нет.</td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- Редактор роли: та же форма для новой и для правки существующей.
         Разделять их незачем — отличается только заблокированный код. -->
    <section v-if="editing" class="panel">
      <div class="panel-head">
        <h2>{{ isNew ? 'Новая роль' : `Роль «${draft.name || draft.code}»` }}</h2>
        <button type="button" class="btn-link" @click="cancelEdit">закрыть</button>
      </div>

      <div class="form-grid">
        <label class="field">
          <span>Код</span>
          <input
            v-model="draft.code"
            class="input mono"
            placeholder="FINANCE"
            :disabled="!isNew"
          />
          <small>Заглавные латинские буквы, цифры и подчёркивание. После создания не меняется.</small>
        </label>
        <label class="field">
          <span>Название</span>
          <input v-model="draft.name" class="input" placeholder="Финансист" :disabled="readOnly" />
        </label>
        <label class="field wide">
          <span>Описание</span>
          <input
            v-model="draft.description"
            class="input"
            placeholder="Видит сверку и заявки на вывод, ничего не меняет."
            :disabled="readOnly"
          />
        </label>
      </div>

      <p v-if="!isNew && draft.code === 'ADMIN'" class="hint">
        У администратора все права всегда, включая те, что появятся в следующих
        версиях. Список ниже показан для справки и не редактируется.
      </p>

      <div v-for="group in groups" :key="group.name" class="perm-group">
        <div class="perm-group-head">
          <h3>{{ group.name }}</h3>
          <button
            v-if="!readOnly"
            type="button"
            class="btn-link"
            @click="toggleGroup(group)"
          >
            {{ groupFullyGranted(group) ? 'снять все' : 'выдать все' }}
          </button>
        </div>
        <table class="perm-table">
          <thead>
            <tr>
              <th>Раздел</th>
              <th v-for="action in catalog.actions" :key="action.key">{{ action.label }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="section in group.sections" :key="section.key">
              <td>
                <div>{{ section.label }}</div>
                <div v-if="section.hint" class="muted-text">{{ section.hint }}</div>
              </td>
              <td v-for="action in catalog.actions" :key="action.key" class="check-cell">
                <!-- Пустая клетка там, где действие в разделе невозможно: у
                     журнала проводок нечего добавлять и удалять. -->
                <input
                  v-if="section.actions.includes(action.key)"
                  type="checkbox"
                  :checked="hasPermission(section.key, action.key)"
                  :disabled="readOnly"
                  @change="togglePermission(section, action.key)"
                />
                <span v-else class="dash">—</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="!readOnly" class="row save-row">
        <button type="button" class="btn-primary" :disabled="saving" @click="save">
          {{ isNew ? 'Создать роль' : 'Сохранить' }}
        </button>
        <span class="muted-text">Выдано прав: {{ draft.permissions.length }}</span>
      </div>
      <p v-if="!readOnly" class="hint">
        Изменение прав завершает сессии носителей роли: панель должна перерисоваться
        под новые права, а не ждать, пока истечёт токен.
      </p>
    </section>

    <!-- Кому подключена роль. -->
    <section v-if="usersFor" class="panel">
      <div class="panel-head">
        <h2>Кому подключена роль «{{ usersFor.name }}»</h2>
        <button type="button" class="btn-link" @click="closeUsers">закрыть</button>
      </div>

      <div class="row">
        <input
          v-model="userSearch"
          class="input"
          placeholder="Телефон, почта или фамилия"
          @keyup.enter="refreshUsers"
        />
        <button type="button" class="btn-secondary" @click="refreshUsers">Найти</button>
        <span class="muted-text">Всего: {{ usersTotal }}</span>
      </div>

      <table class="role-table">
        <thead>
          <tr>
            <th>Телефон</th>
            <th>Имя</th>
            <th>Почта</th>
            <th>Статус</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in roleUsers" :key="u.id">
            <td class="mono">{{ u.phone }}</td>
            <td>
              {{ u.full_name || '—' }}
              <span v-if="u.is_primary" class="badge muted" title="Роль по умолчанию: с неё открывается дашборд">
                основная
              </span>
            </td>
            <td>{{ u.email || '—' }}</td>
            <td>{{ u.status === 'BANNED' ? 'заблокирован' : 'активен' }}</td>
            <td class="actions">
              <button
                v-if="canEdit"
                type="button"
                class="btn-link danger"
                @click="removeUser(u)"
              >
                снять роль
              </button>
            </td>
          </tr>
          <tr v-if="!roleUsers.length">
            <td colspan="5" class="empty">Роль никому не подключена.</td>
          </tr>
        </tbody>
      </table>

      <div v-if="usersTotal > roleUsers.length" class="row">
        <button type="button" class="btn-secondary" @click="loadMoreUsers">Показать ещё</button>
      </div>

      <div v-if="canEdit" class="assign-block">
        <h3>Подключить роль</h3>
        <p class="muted-text">
          Найдите пользователя по телефону — тому же полю, по которому ищет список
          пользователей.
        </p>
        <div class="row">
          <input
            v-model="assignSearch"
            class="input"
            placeholder="Телефон"
            @keyup.enter="searchCandidates"
          />
          <button type="button" class="btn-secondary" :disabled="!assignSearch" @click="searchCandidates">
            Найти
          </button>
        </div>
        <ul v-if="candidates.length" class="candidates">
          <li v-for="c in candidates" :key="c.id">
            <span class="mono">{{ c.phone }}</span>
            <span>{{ [c.last_name, c.first_name].filter(Boolean).join(' ') || '—' }}</span>
            <button type="button" class="btn-link" @click="addUser(c)">подключить</button>
          </li>
        </ul>
        <p v-else-if="searchedCandidates" class="muted-text">Никого не найдено.</p>
      </div>
    </section>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed, onMounted, reactive, ref } from 'vue'
import api from '../../services/api'
import { useAuthStore } from '../../stores/auth-store'
import {
  assignRole,
  createRole,
  deleteRole,
  getPermissionCatalog,
  getRoles,
  getRoleUsers,
  unassignRole,
  updateRole,
  PermissionAction,
  PermissionCatalog,
  PermissionSection,
  Role,
  RoleUser,
} from '../../api/roles'

// Одна страница отвечает на три вопроса про роль: какие роли есть, что каждая
// открывает и кто её носит. Они разделены на три панели, а не на три экрана,
// потому что задаются подряд: администратор заводит роль, тут же раздаёт ей
// разделы и тут же подключает первому человеку.
const PAGE_SIZE = 20

export default defineComponent({
  name: 'AdminRoles',
  setup() {
    const authStore = useAuthStore()

    const roles = ref<Role[]>([])
    const catalog = ref<PermissionCatalog>({ sections: [], actions: [] })
    const loading = ref(false)
    const saving = ref(false)
    const errorMsg = ref('')
    const successMsg = ref('')

    const editing = ref(false)
    const isNew = ref(false)
    const selected = ref<Role | null>(null)
    const draft = reactive({ code: '', name: '', description: '', permissions: [] as string[] })

    const usersFor = ref<Role | null>(null)
    const roleUsers = ref<RoleUser[]>([])
    const usersTotal = ref(0)
    const userSearch = ref('')

    const assignSearch = ref('')
    const candidates = ref<any[]>([])
    const searchedCandidates = ref(false)

    const canCreate = computed(() => authStore.can('roles.create'))
    const canEdit = computed(() => authStore.can('roles.edit'))
    const canDelete = computed(() => authStore.can('roles.delete'))
    // Права администратора не редактируются: они полные по определению. Роль
    // показывается в режиме чтения, чтобы было видно, что именно он может.
    const readOnly = computed(() => !canEdit.value || (!isNew.value && draft.code === 'ADMIN'))

    // Разделы группируются так же, как пункты в боковой панели, — матрица прав
    // тогда читается как само меню.
    const groups = computed(() => {
      const order: string[] = []
      const byGroup = new Map<string, PermissionSection[]>()
      for (const section of catalog.value.sections) {
        if (!byGroup.has(section.group)) {
          byGroup.set(section.group, [])
          order.push(section.group)
        }
        byGroup.get(section.group)!.push(section)
      }
      return order.map((name) => ({ name, sections: byGroup.get(name)! }))
    })

    const load = async () => {
      loading.value = true
      errorMsg.value = ''
      try {
        const [loadedCatalog, loadedRoles] = await Promise.all([getPermissionCatalog(), getRoles()])
        catalog.value = loadedCatalog
        roles.value = loadedRoles
        // Открытая карточка обновляется вместе со списком, иначе после сохранения
        // на экране осталось бы прежнее состояние.
        if (usersFor.value) {
          const fresh = loadedRoles.find((r) => r.code === usersFor.value!.code)
          if (fresh) usersFor.value = fresh
        }
      } catch (e) {
        errorMsg.value = message(e, 'Не удалось загрузить роли.')
      } finally {
        loading.value = false
      }
    }

    const permissionSummary = (role: Role) => {
      if (role.code === 'ADMIN') return 'все разделы'
      if (!role.permissions.length) return 'нет доступа в панель'
      const sections = new Set(role.permissions.map((p) => p.split('.')[0]))
      return `${sections.size} раздел(ов), ${role.permissions.length} прав(а)`
    }

    const startCreate = () => {
      isNew.value = true
      editing.value = true
      selected.value = null
      draft.code = ''
      draft.name = ''
      draft.description = ''
      draft.permissions = []
    }

    const startEdit = (role: Role) => {
      isNew.value = false
      editing.value = true
      selected.value = role
      draft.code = role.code
      draft.name = role.name
      draft.description = role.description
      draft.permissions = [...role.permissions]
    }

    const cancelEdit = () => {
      editing.value = false
      selected.value = null
    }

    const hasPermission = (sectionKey: string, action: PermissionAction) =>
      draft.permissions.includes(`${sectionKey}.${action}`)

    const togglePermission = (section: PermissionSection, action: PermissionAction) => {
      const code = `${section.key}.${action}`
      if (draft.permissions.includes(code)) {
        draft.permissions = draft.permissions.filter((p) => p !== code)
        // Снятый просмотр забирает с собой остальные действия в разделе:
        // «менять, но не видеть» — это право, которым нельзя воспользоваться,
        // а в матрице оно выглядело бы выданным.
        if (action === 'view') {
          draft.permissions = draft.permissions.filter((p) => !p.startsWith(`${section.key}.`))
        }
        return
      }
      draft.permissions = [...draft.permissions, code]
      // И наоборот: любое действие подразумевает просмотр раздела, иначе
      // страницы, на которой его делают, просто не будет в меню.
      if (action !== 'view' && section.actions.includes('view') && !hasPermission(section.key, 'view')) {
        draft.permissions = [...draft.permissions, `${section.key}.view`]
      }
    }

    const groupCodes = (group: { sections: PermissionSection[] }) =>
      group.sections.flatMap((s) => s.actions.map((a) => `${s.key}.${a}`))

    const groupFullyGranted = (group: { sections: PermissionSection[] }) =>
      groupCodes(group).every((code) => draft.permissions.includes(code))

    const toggleGroup = (group: { sections: PermissionSection[] }) => {
      const codes = groupCodes(group)
      if (groupFullyGranted(group)) {
        draft.permissions = draft.permissions.filter((p) => !codes.includes(p))
      } else {
        const set = new Set([...draft.permissions, ...codes])
        draft.permissions = [...set]
      }
    }

    const save = async () => {
      saving.value = true
      errorMsg.value = ''
      successMsg.value = ''
      try {
        if (isNew.value) {
          const created = await createRole({
            code: draft.code.trim().toUpperCase(),
            name: draft.name.trim(),
            description: draft.description.trim(),
            permissions: draft.permissions,
          })
          successMsg.value = `Роль «${created.name}» создана.`
          isNew.value = false
          draft.code = created.code
        } else {
          const updated = await updateRole(draft.code, {
            name: draft.name.trim(),
            description: draft.description.trim(),
            permissions: draft.permissions,
          })
          successMsg.value = `Роль «${updated.name}» сохранена.`
        }
        await load()
      } catch (e) {
        errorMsg.value = message(e, 'Не удалось сохранить роль.')
      } finally {
        saving.value = false
      }
    }

    const askDelete = async (role: Role) => {
      const holders = role.user_count
        ? ` Роль будет снята с ${role.user_count} пользовател(я/ей).`
        : ''
      if (!window.confirm(`Удалить роль «${role.name}»?${holders}`)) return
      errorMsg.value = ''
      try {
        await deleteRole(role.code)
        successMsg.value = `Роль «${role.name}» удалена.`
        if (selected.value?.code === role.code) cancelEdit()
        if (usersFor.value?.code === role.code) closeUsers()
        await load()
      } catch (e) {
        errorMsg.value = message(e, 'Не удалось удалить роль.')
      }
    }

    const openUsers = async (role: Role) => {
      usersFor.value = role
      userSearch.value = ''
      candidates.value = []
      searchedCandidates.value = false
      await loadUsers()
    }

    const closeUsers = () => {
      usersFor.value = null
      roleUsers.value = []
      usersTotal.value = 0
    }

    const loadUsers = async (append = false) => {
      if (!usersFor.value) return
      try {
        const { users, total } = await getRoleUsers(usersFor.value.code, {
          search: userSearch.value.trim(),
          limit: PAGE_SIZE,
          offset: append ? roleUsers.value.length : 0,
        })
        roleUsers.value = append ? [...roleUsers.value, ...users] : users
        usersTotal.value = total
      } catch (e) {
        errorMsg.value = message(e, 'Не удалось загрузить носителей роли.')
      }
    }

    // Отдельные обёртки для шаблона: обработчик события передал бы в loadUsers
    // объект события вместо флага «дописать к списку».
    const refreshUsers = () => loadUsers(false)
    const loadMoreUsers = () => loadUsers(true)

    const searchCandidates = async () => {
      searchedCandidates.value = true
      try {
        const res = await api.get('/admin/users', {
          params: { search: assignSearch.value.trim(), page: 1, limit: 10 },
        })
        candidates.value = res.data.users || []
      } catch (e) {
        errorMsg.value = message(e, 'Не удалось найти пользователей.')
      }
    }

    const addUser = async (candidate: { id: string; phone: string }) => {
      if (!usersFor.value) return
      errorMsg.value = ''
      try {
        await assignRole(usersFor.value.code, candidate.id)
        successMsg.value = `Роль подключена: ${candidate.phone}.`
        candidates.value = candidates.value.filter((c) => c.id !== candidate.id)
        await Promise.all([loadUsers(), load()])
      } catch (e) {
        errorMsg.value = message(e, 'Не удалось подключить роль.')
      }
    }

    const removeUser = async (user: RoleUser) => {
      if (!usersFor.value) return
      if (!window.confirm(`Снять роль «${usersFor.value.name}» с ${user.phone}?`)) return
      errorMsg.value = ''
      try {
        await unassignRole(usersFor.value.code, user.id)
        successMsg.value = `Роль снята: ${user.phone}.`
        await Promise.all([loadUsers(), load()])
      } catch (e) {
        errorMsg.value = message(e, 'Не удалось снять роль.')
      }
    }

    // Тексты отказов бэкенд присылает на русском и адресует администратору
    // («нельзя снять роль с последнего администратора»), поэтому показываются
    // как есть, а запасной вариант нужен только для сетевого сбоя.
    const message = (e: unknown, fallback: string) => {
      const data = (e as { response?: { data?: unknown } })?.response?.data
      const text = typeof data === 'string' ? data.trim() : ''
      return text || fallback
    }

    onMounted(load)

    return {
      roles,
      catalog,
      groups,
      loading,
      saving,
      errorMsg,
      successMsg,
      editing,
      isNew,
      selected,
      draft,
      readOnly,
      canCreate,
      canEdit,
      canDelete,
      usersFor,
      roleUsers,
      usersTotal,
      userSearch,
      assignSearch,
      candidates,
      searchedCandidates,
      load,
      permissionSummary,
      startCreate,
      startEdit,
      cancelEdit,
      hasPermission,
      togglePermission,
      groupFullyGranted,
      toggleGroup,
      save,
      askDelete,
      openUsers,
      closeUsers,
      refreshUsers,
      loadMoreUsers,
      searchCandidates,
      addUser,
      removeUser,
    }
  },
})
</script>

<style scoped>
.roles-admin {
  padding: 4px;
}

.page-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 16px;
}

.page-head h1 {
  font-size: 20px;
  margin: 0 0 6px;
}

.page-sub {
  color: #6b7280;
  font-size: 13px;
  margin: 0;
  max-width: 760px;
}

.btn-refresh {
  border: 1px solid #e5e7eb;
  background: #fff;
  border-radius: 10px;
  padding: 8px 12px;
  cursor: pointer;
}

.alert {
  padding: 10px 12px;
  border-radius: 10px;
  font-size: 13px;
  margin-bottom: 12px;
}

.alert.error {
  background: #fef2f2;
  color: #b91c1c;
}

.alert.success {
  background: #ecfdf5;
  color: #047857;
}

.panel {
  background: #fff;
  border-radius: 14px;
  padding: 16px;
  margin-bottom: 14px;
  border: 1px solid #eef0f4;
}

.panel-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
}

.panel-head h2 {
  font-size: 16px;
  margin: 0;
}

.role-table,
.perm-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.role-table th,
.perm-table th {
  text-align: left;
  color: #6b7280;
  font-weight: 500;
  padding: 8px 10px;
  border-bottom: 1px solid #eef0f4;
}

.role-table td,
.perm-table td {
  padding: 10px;
  border-bottom: 1px solid #f5f6f8;
  vertical-align: top;
}

.role-table tr.selected {
  background: #f8fafc;
}

.perm-table th:not(:first-child),
.check-cell {
  text-align: center;
  width: 96px;
}

.check-cell input {
  width: 16px;
  height: 16px;
  cursor: pointer;
}

.check-cell input:disabled {
  cursor: default;
}

.dash {
  color: #d1d5db;
}

.perm-group {
  margin-top: 14px;
}

.perm-group-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.perm-group-head h3 {
  font-size: 13px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: #6b7280;
  margin: 0;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 13px;
}

.field.wide {
  grid-column: 1 / -1;
}

.field small {
  color: #9ca3af;
  font-size: 11px;
}

.input {
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  padding: 8px 10px;
  font-size: 13px;
}

.input:disabled {
  background: #f9fafb;
  color: #6b7280;
}

.mono {
  font-family: ui-monospace, monospace;
  letter-spacing: 0.04em;
}

.row {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}

.save-row {
  margin-top: 16px;
}

.btn-primary {
  border: none;
  background: #111827;
  color: #fff;
  border-radius: 10px;
  padding: 8px 16px;
  font-size: 13px;
  cursor: pointer;
}

.btn-primary:disabled {
  opacity: 0.5;
  cursor: default;
}

.btn-secondary {
  border: 1px solid #e5e7eb;
  background: #fff;
  border-radius: 10px;
  padding: 8px 14px;
  font-size: 13px;
  cursor: pointer;
}

.btn-link {
  border: none;
  background: none;
  color: #2563eb;
  font-size: 13px;
  cursor: pointer;
  padding: 0 6px 0 0;
}

.btn-link.danger {
  color: #b91c1c;
}

.actions {
  white-space: nowrap;
}

.badge {
  display: inline-block;
  border-radius: 999px;
  padding: 1px 8px;
  font-size: 11px;
  margin-left: 6px;
}

.badge.muted {
  background: #f3f4f6;
  color: #6b7280;
}

.muted-text {
  color: #9ca3af;
  font-size: 12px;
}

.hint {
  color: #6b7280;
  font-size: 12px;
  margin: 10px 0 0;
}

.empty {
  color: #9ca3af;
  text-align: center;
  padding: 18px;
}

.assign-block {
  margin-top: 18px;
  border-top: 1px solid #eef0f4;
  padding-top: 14px;
}

.assign-block h3 {
  font-size: 14px;
  margin: 0 0 4px;
}

.candidates {
  list-style: none;
  margin: 10px 0 0;
  padding: 0;
  font-size: 13px;
}

.candidates li {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 6px 0;
  border-bottom: 1px solid #f5f6f8;
}
</style>
