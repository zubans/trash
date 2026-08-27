<template>
  <div class="service-catalog-page">
    <!-- Header -->
    <div class="page-header">
      <div>
        <h1 class="page-title">
          <i class="ph-fill ph-list-dashes" style="color: #5c60f5;"></i>
          Каталог услуг
        </h1>
        <div class="page-subtitle">Управление категориями и вариантами оказываемых услуг</div>
      </div>
      <div class="header-actions">
        <label class="show-deleted">
          <input v-model="showDeleted" type="checkbox" @change="fetchTree" />
          <span>Показывать удалённые</span>
        </label>
        <button type="button" class="btn-primary" @click="openCreateModal(null)">
          <i class="ph-bold ph-plus"></i> Добавить категорию
        </button>
      </div>
    </div>

    <!-- Alert Messages -->
    <div v-if="successMsg" class="catalog-alert alert-success mb-3">
      <i class="ph-bold ph-check-circle alert-icon"></i>
      <span>{{ successMsg }}</span>
      <button type="button" class="btn-dismiss" @click="successMsg = ''"><i class="ph ph-x"></i></button>
    </div>

    <div v-if="errorMsg" class="catalog-alert alert-danger mb-3">
      <i class="ph-bold ph-warning-circle alert-icon"></i>
      <span>{{ errorMsg }}</span>
      <button type="button" class="btn-dismiss" @click="errorMsg = ''"><i class="ph ph-x"></i></button>
    </div>

    <!-- Main Catalog Card -->
    <div class="catalog-card">
      <!-- Loading State -->
      <div v-if="loading" class="loading-state py-5">
        <div class="spinner"></div>
        <span>Загрузка каталога...</span>
      </div>

      <!-- Tree View -->
      <div v-else class="tree-container">
        <div v-if="tree.length === 0" class="empty-tree-state">
          <i class="ph-fill ph-folders empty-icon"></i>
          <h3>Каталог пуст</h3>
          <p>Создайте первую категорию, чтобы сформировать список услуг</p>
          <button type="button" class="btn-primary mt-3" @click="openCreateModal(null)">
            <i class="ph-bold ph-plus"></i> Создать категорию
          </button>
        </div>

        <service-node-tree
          v-else
          :nodes="tree"
          @create="openCreateModal"
          @edit="openEditModal"
          @delete="confirmDelete"
          @restore="restoreNode"
        />
      </div>
    </div>

    <!-- Custom Form Modal Overlay -->
    <div v-if="showFormModal" class="catalog-modal-overlay" @click.self="showFormModal = false">
      <service-node-form
        :node="editingNode"
        :initial-parent-id="defaultParentId"
        :parent-options="parentOptions"
        @save="saveNode"
        @cancel="showFormModal = false"
      />
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, computed } from 'vue'
import type { ServiceNode } from '../../api/services'
import {
  getAdminServiceNodes,
  createServiceNode,
  updateServiceNode,
  deleteServiceNode,
  restoreServiceNode,
} from '../../api/admin-services'
import ServiceNodeTree from './ServiceNodeTree.vue'
import ServiceNodeForm from './ServiceNodeForm.vue'

interface TreeItem {
  node: ServiceNode
  children: TreeItem[]
}

export default defineComponent({
  name: 'ServiceCatalog',
  components: { ServiceNodeTree, ServiceNodeForm },
  setup() {
    const tree = ref<TreeItem[]>([])
    const loading = ref(false)
    const showFormModal = ref(false)
    const editingNode = ref<ServiceNode | null>(null)
    const defaultParentId = ref<string | null>(null)
    const successMsg = ref('')
    const errorMsg = ref('')
    const showDeleted = ref(false)

    const flatten = (items: TreeItem[]): ServiceNode[] => {
      const out: ServiceNode[] = []
      for (const item of items) {
        out.push(item.node)
        out.push(...flatten(item.children))
      }
      return out
    }

    // A deleted category is listed in the tree but cannot take new children.
    const parentOptions = computed(() =>
      flatten(tree.value)
        .filter((n) => n.node_type === 'CATEGORY' && !n.deleted_at)
        .map((n) => ({
          label: n.name['ru'] || n.code,
          value: n.id,
        }))
    )

    const fetchTree = async () => {
      loading.value = true
      try {
        tree.value = await getAdminServiceNodes(showDeleted.value)
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Не удалось загрузить каталог'
      } finally {
        loading.value = false
      }
    }

    const openCreateModal = (parentId: string | null) => {
      editingNode.value = null
      defaultParentId.value = parentId
      showFormModal.value = true
    }

    const openEditModal = (node: ServiceNode) => {
      editingNode.value = node
      showFormModal.value = true
    }

    const saveNode = async (payload: any) => {
      successMsg.value = ''
      errorMsg.value = ''
      try {
        if (editingNode.value) {
          await updateServiceNode(editingNode.value.id, payload)
          successMsg.value = 'Элемент каталога успешно обновлен'
        } else {
          await createServiceNode(payload)
          successMsg.value = 'Новый элемент успешно создан'
        }
        showFormModal.value = false
        await fetchTree()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка при сохранении'
      }
    }

    // Deletion is soft on the backend: the element leaves the catalog, the
    // orders placed for it keep their service, and it can be restored.
    const confirmDelete = async (node: ServiceNode) => {
      const title = node.name['ru'] || node.code
      if (
        !confirm(
          `Удалить "${title}"?\n\nЭлемент будет скрыт из приложения. ` +
            'История заказов сохранится, элемент можно восстановить.'
        )
      ) {
        return
      }
      successMsg.value = ''
      errorMsg.value = ''
      try {
        const result = await deleteServiceNode(node.id)
        successMsg.value = result?.had_orders
          ? 'Элемент скрыт из каталога. Ранее созданные заказы сохранены.'
          : 'Элемент удален из каталога'
        await fetchTree()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка при удалении'
      }
    }

    const restoreNode = async (node: ServiceNode) => {
      successMsg.value = ''
      errorMsg.value = ''
      try {
        await restoreServiceNode(node.id)
        successMsg.value =
          'Элемент восстановлен. Включите «Активно в приложении», чтобы он снова появился у пользователей.'
        await fetchTree()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка при восстановлении'
      }
    }

    onMounted(fetchTree)

    return {
      tree,
      loading,
      showFormModal,
      editingNode,
      defaultParentId,
      parentOptions,
      successMsg,
      errorMsg,
      showDeleted,
      fetchTree,
      openCreateModal,
      openEditModal,
      saveNode,
      confirmDelete,
      restoreNode,
    }
  },
})
</script>

<style scoped>
.service-catalog-page {
  padding: 8px 0;
  max-width: 1200px;
  margin: 0 auto;
  font-family: 'Outfit', sans-serif;
  color: #0f172a;
}

/* Header */
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
  max-width: 1200px;
  margin-inline: auto;
}

.page-title {
  font-size: 28px;
  font-weight: 700;
  color: #0f172a;
  letter-spacing: -0.5px;
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 0;
}

.page-subtitle {
  font-size: 14px;
  font-weight: 400;
  color: #64748b;
  margin-top: 4px;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 16px;
}

.show-deleted {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 500;
  color: #64748b;
  cursor: pointer;
  white-space: nowrap;
}

.show-deleted input {
  width: 16px;
  height: 16px;
  accent-color: #5c60f5;
  cursor: pointer;
}

.btn-primary {
  background: #5c60f5;
  color: #ffffff;
  border: none;
  padding: 12px 20px;
  border-radius: 12px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  box-shadow: 0 4px 12px rgba(92, 96, 245, 0.2);
  transition: all 0.2s ease-in-out;
}

.btn-primary:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 16px rgba(92, 96, 245, 0.3);
}

/* Catalog Card */
.catalog-card {
  background: #ffffff;
  border-radius: 24px;
  box-shadow: 0 4px 24px rgba(15, 23, 42, 0.04);
  padding: 32px;
  max-width: 1200px;
  margin: 0 auto;
}

.tree-container {
  display: flex;
  flex-direction: column;
}

/* Alert Messages */
.catalog-alert {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 18px;
  border-radius: 12px;
  font-size: 14px;
  font-weight: 500;
}

.alert-success {
  background: #ecfdf5;
  color: #065f46;
  border: 1px solid #a7f3d0;
}

.alert-danger {
  background: #fef2f2;
  color: #991b1b;
  border: 1px solid #fecaca;
}

.alert-icon {
  font-size: 18px;
}

.btn-dismiss {
  margin-left: auto;
  background: transparent;
  border: none;
  color: inherit;
  font-size: 16px;
  cursor: pointer;
  opacity: 0.7;
}

.btn-dismiss:hover { opacity: 1; }

.loading-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  color: #64748b;
}

.spinner {
  width: 32px;
  height: 32px;
  border: 3px solid rgba(92, 96, 245, 0.2);
  border-top-color: #5c60f5;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.empty-tree-state {
  text-align: center;
  padding: 48px 24px;
  background: #ffffff;
  border-radius: 20px;
  border: 1px dashed #cbd5e1;
}

.empty-icon {
  font-size: 48px;
  color: #94a3b8;
  margin-bottom: 12px;
}

.catalog-modal-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(15, 23, 42, 0.6);
  backdrop-filter: blur(4px);
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  overflow-y: auto;
}

@media (max-width: 768px) {
  .page-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 16px;
  }
  .header-actions {
    width: 100%;
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }
  .btn-primary {
    width: 100%;
    justify-content: center;
  }
  .catalog-card {
    padding: 16px;
    border-radius: 16px;
  }
}
</style>

