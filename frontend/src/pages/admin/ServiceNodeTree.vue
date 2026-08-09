<template>
  <ul class="node-tree">
    <li v-for="item in nodes" :key="item.node.id" class="node-item">
      <div class="node-row d-flex align-items-center justify-content-between">
        <div>
          <span class="font-bold">{{ item.node.name['ru'] || item.node.code }}</span>
          <span class="text-xs text-secondary ml-2">({{ item.node.code }})</span>
          <va-badge
            :color="item.node.node_type === 'CATEGORY' ? 'info' : 'success'"
            size="small"
            class="ml-2"
          >
            {{ item.node.node_type }}
          </va-badge>
          <va-badge v-if="!item.node.is_active" color="secondary" size="small" class="ml-2">
            {{ $t('admin.inactive') }}
          </va-badge>
        </div>
        <div class="d-flex gap-1">
          <va-button color="primary" size="small" outline @click="$emit('create', item.node.id)">
            {{ $t('admin.addChild') }}
          </va-button>
          <va-button color="warning" size="small" outline @click="$emit('edit', item.node)">
            {{ $t('admin.edit') }}
          </va-button>
          <va-button color="danger" size="small" outline @click="$emit('delete', item.node)">
            {{ $t('admin.delete') }}
          </va-button>
        </div>
      </div>
      <service-node-tree
        v-if="item.children && item.children.length > 0"
        :nodes="item.children"
        @create="$emit('create', $event)"
        @edit="$emit('edit', $event)"
        @delete="$emit('delete', $event)"
      />
    </li>
  </ul>
</template>

<script lang="ts">
import { defineComponent } from 'vue'

export default defineComponent({
  name: 'ServiceNodeTree',
  props: {
    nodes: {
      type: Array as any,
      required: true,
    },
  },
  emits: ['create', 'edit', 'delete'],
})
</script>

<style scoped>
.node-tree {
  list-style: none;
  padding-left: 0;
}
.node-item {
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  padding: 12px;
  margin-bottom: 8px;
  background: #fff;
}
.node-row {
  margin-bottom: 8px;
}
.ml-2 {
  margin-left: 8px;
}
</style>
