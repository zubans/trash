<template>
  <div class="service-catalog-page">
    <!-- Top Header -->
    <div class="catalog-header mb-4">
      <div>
        <h1 class="page-title">Каталог услуг</h1>
        <p class="page-subtitle">Управление категориями и вариантами оказываемых услуг</p>
      </div>
      <button type="button" class="btn-create-root" @click="openCreateModal(null)">
        <i class="ph-bold ph-plus me-1"></i> Добавить категорию
      </button>
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

    <!-- Loading -->
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
        <button type="button" class="btn-create-root mt-2" @click="openCreateModal(null)">
          <i class="ph-bold ph-plus me-1"></i> Создать категорию
        </button>
      </div>

      <service-node-tree
        v-else
        :nodes="tree"
        @create="openCreateModal"
        @edit="openEditModal"
        @delete="confirmDelete"
      />
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
import { useI18n } from 'vue-i18n'
import type { ServiceNode } from '../../api/services'
import {
  getAdminServiceNodes,
  createServiceNode,
  updateServiceNode,
  deleteServiceNode,
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
    const { t } = useI18n()
    const tree = ref<TreeItem[]>([])
    const loading = ref(false)
    const showFormModal = ref(false)
    const editingNode = ref<ServiceNode | null>(null)
    const defaultParentId = ref<string | null>(null)
    const successMsg = ref('')
    const errorMsg = ref('')

    const flatten = (items: TreeItem[]): ServiceNode[] => {
      const out: ServiceNode[] = []
      for (const item of items) {
        out.push(item.node)
        out.push(...flatten(item.children))
      }
      return out
    }

    const parentOptions = computed(() =>
      flatten(tree.value)
        .filter((n) => n.node_type === 'CATEGORY')
        .map((n) => ({
          label: n.name['ru'] || n.code,
          value: n.id,
        }))
    )

    const fetchTree = async () => {
      loading.value = true
      try {
        tree.value = await getAdminServiceNodes()
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

    const confirmDelete = async (node: ServiceNode) => {
      if (!confirm(`Вы действительно хотите удалить "${node.name['ru'] || node.code}"?`)) {
        return
      }
      successMsg.value = ''
      errorMsg.value = ''
      try {
        await deleteServiceNode(node.id)
        successMsg.value = 'Элемент успешно удален'
        await fetchTree()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка при удалении'
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
      openCreateModal,
      openEditModal,
      saveNode,
      confirmDelete,
    }
  },
})
</script>

<style scoped>
.service-catalog-page {
  padding: 24px;
}

.catalog-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.page-title {
  font-size: 24px;
  font-weight: 800;
  color: #0f172a;
  margin: 0;
  letter-spacing: -0.5px;
}

.page-subtitle {
  font-size: 14px;
  color: #64748b;
  margin: 4px 0 0 0;
}

.btn-create-root {
  background: #5c60f5;
  color: #ffffff;
  font-size: 14px;
  font-weight: 600;
  padding: 10px 20px;
  border-radius: 12px;
  border: none;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  box-shadow: 0 4px 12px rgba(92, 96, 245, 0.2);
  transition: all 0.2s ease;
}

.btn-create-root:hover {
  background: #4f52e6;
  box-shadow: 0 6px 16px rgba(92, 96, 245, 0.3);
}

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
  border: 1px solid #e2e8f0;
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
</style>
