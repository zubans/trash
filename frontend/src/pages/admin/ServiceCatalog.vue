<template>
  <div class="service-catalog">
    <h1 class="va-h3 mb-4">{{ $t('admin.serviceCatalog') }}</h1>

    <va-alert v-if="successMsg" color="success" class="mb-3" closeable @dismissed="successMsg = ''">
      {{ successMsg }}
    </va-alert>
    <va-alert v-if="errorMsg" color="danger" class="mb-3" closeable @dismissed="errorMsg = ''">
      {{ errorMsg }}
    </va-alert>

    <va-button color="primary" class="mb-4" @click="openCreateModal(null)">
      {{ $t('admin.addCategory') }}
    </va-button>

    <div v-if="loading" class="text-center py-5">
      <va-progress-circle indeterminate />
    </div>

    <div v-else>
      <service-node-tree
        :nodes="tree"
        @create="openCreateModal"
        @edit="openEditModal"
        @delete="confirmDelete"
      />
    </div>

    <va-modal v-model="showFormModal" :title="formTitle" hide-default-actions>
      <service-node-form
        :node="editingNode"
        :initial-parent-id="defaultParentId"
        :parent-options="parentOptions"
        @save="saveNode"
        @cancel="showFormModal = false"
      />
    </va-modal>
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

    const formTitle = computed(() =>
      editingNode.value ? t('admin.editNode') : t('admin.addNode')
    )

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
        errorMsg.value = err.response?.data || 'Failed to load catalog'
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
          successMsg.value = t('admin.nodeUpdated')
        } else {
          await createServiceNode(payload)
          successMsg.value = t('admin.nodeCreated')
        }
        showFormModal.value = false
        await fetchTree()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Failed to save node'
      }
    }

    const confirmDelete = async (node: ServiceNode) => {
      if (!confirm(`${t('admin.confirmDeleteNode')} "${node.name['ru'] || node.code}"?`)) {
        return
      }
      successMsg.value = ''
      errorMsg.value = ''
      try {
        await deleteServiceNode(node.id)
        successMsg.value = t('admin.nodeDeleted')
        await fetchTree()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Failed to delete node'
      }
    }

    onMounted(fetchTree)

    return {
      tree,
      loading,
      showFormModal,
      formTitle,
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
.service-catalog {
  padding: 10px;
}
</style>
