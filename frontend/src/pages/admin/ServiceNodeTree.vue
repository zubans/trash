<template>
  <div class="tree-nodes-list">
    <div
      v-for="item in nodes"
      :key="item.node.id"
      class="tree-node"
      :class="{ collapsed: isCollapsed(item.node.id) }"
    >
      <div class="node-row" :class="{ deleted: isDeleted(item.node) }">
        <!-- Ручка перетаскивания -->
        <i class="ph-bold ph-dots-six-vertical drag-handle" title="Перетащить"></i>

        <!-- Шеврон (развернуть/свернуть) -->
        <i
          class="ph-bold ph-caret-down chevron"
          :class="{ hidden: !item.children || item.children.length === 0 }"
          @click.stop="toggleNode(item.node.id)"
        ></i>

        <!-- Иконка узла -->
        <i
          v-if="item.node.node_type === 'CATEGORY'"
          class="ph-fill node-icon cat"
          :class="isCollapsed(item.node.id) ? 'ph-folder' : 'ph-folder-notch-open'"
        ></i>
        <i
          v-else
          class="ph-fill ph-tag node-icon var"
        ></i>

        <!-- Содержимое узла -->
        <div class="node-content">
          <span class="node-title">{{ item.node.name['ru'] || item.node.code }}</span>
          <span class="node-code">{{ item.node.code }}</span>

          <div v-if="item.node.is_auction" class="badge-auction">
            <i class="ph-fill ph-gavel"></i> Аукцион
          </div>

          <div v-if="isDeleted(item.node)" class="badge-deleted" title="Удалено: скрыто из приложения, история заказов сохранена">
            <i class="ph-fill ph-archive"></i> Удалено
          </div>

          <div
            v-else
            class="status-dot"
            :class="{ inactive: !item.node.is_active }"
            :title="item.node.is_active ? 'Активен' : 'Отключен'"
          ></div>
        </div>

        <!-- Цена (для вариантов, выровнена вправо) -->
        <div v-if="item.node.node_type === 'VARIANT'" class="node-price">
          {{ item.node.is_auction ? '—' : formatPrice(item.node.base_price) }}
        </div>

        <!-- Действия над узлом -->
        <div class="node-actions">
          <!-- Удалённый узел сначала восстанавливают, и только потом его можно править. -->
          <button
            v-if="isDeleted(item.node)"
            type="button"
            class="btn-action restore"
            title="Восстановить"
            @click.stop="$emit('restore', item.node)"
          >
            <i class="ph-bold ph-arrow-counter-clockwise"></i>
          </button>
          <template v-else>
            <button
              v-if="item.node.node_type === 'CATEGORY'"
              type="button"
              class="btn-action add"
              title="Добавить подэлемент"
              @click.stop="$emit('create', item.node.id)"
            >
              <i class="ph-bold ph-plus"></i>
            </button>
            <button
              type="button"
              class="btn-action"
              title="Редактировать"
              @click.stop="$emit('edit', item.node)"
            >
              <i class="ph-bold ph-pencil-simple"></i>
            </button>
            <button
              type="button"
              class="btn-action delete"
              title="Удалить"
              @click.stop="$emit('delete', item.node)"
            >
              <i class="ph-bold ph-trash"></i>
            </button>
          </template>
        </div>
      </div>

      <!-- Потомки -->
      <div v-if="item.children && item.children.length > 0" class="tree-children">
        <service-node-tree
          :nodes="item.children"
          @create="$emit('create', $event)"
          @edit="$emit('edit', $event)"
          @delete="$emit('delete', $event)"
          @restore="$emit('restore', $event)"
        />
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref } from 'vue'

export default defineComponent({
  name: 'ServiceNodeTree',
  props: {
    nodes: {
      type: Array as any,
      required: true,
    },
  },
  emits: ['create', 'edit', 'delete', 'restore'],
  setup() {
    const collapsedMap = ref<Record<string, boolean>>({})

    const isCollapsed = (id: string) => !!collapsedMap.value[id]

    const toggleNode = (id: string) => {
      collapsedMap.value[id] = !collapsedMap.value[id]
    }

    const isDeleted = (node: { deleted_at?: string | null }) => !!node.deleted_at

    const formatPrice = (val: number | string | null | undefined) => {
      if (val === null || val === undefined || val === '') return '—'
      const num = typeof val === 'number' ? val : parseFloat(String(val))
      if (isNaN(num)) return `${val} ₽`
      return `${num.toFixed(2)} ₽`
    }

    return {
      isCollapsed,
      toggleNode,
      isDeleted,
      formatPrice,
    }
  },
})
</script>

<style scoped>
.tree-nodes-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.tree-node {
  display: flex;
  flex-direction: column;
}

/* Потомки дерева */
.tree-children {
  margin-left: 22px;
  padding-left: 12px;
  border-left: 1px solid rgba(0, 0, 0, 0.06);
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.tree-node.collapsed > .tree-children {
  display: none;
}

.tree-node.collapsed > .node-row .chevron {
  transform: rotate(-90deg);
}

/* Строка узла */
.node-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border-radius: 8px;
  transition: all 0.2s ease-in-out;
  position: relative;
}

.node-row:hover {
  background: #f8fafc;
}

/* Ручка перетаскивания */
.drag-handle {
  color: #cbd5e1;
  font-size: 20px;
  cursor: grab;
  opacity: 0;
  transition: all 0.2s ease-in-out;
  margin-left: -8px;
}

.node-row:hover .drag-handle {
  opacity: 1;
  color: #64748b;
}

.drag-handle:active {
  cursor: grabbing;
}

/* Шеврон */
.chevron {
  color: #64748b;
  font-size: 14px;
  cursor: pointer;
  transition: transform 0.2s ease;
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
}

.chevron:hover {
  background: rgba(0, 0, 0, 0.05);
  color: #0f172a;
}

.chevron.hidden {
  opacity: 0;
  pointer-events: none;
}

/* Иконка узла */
.node-icon {
  font-size: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
}

.node-icon.cat {
  color: #60a5fa;
}

.node-icon.var {
  color: #10b981;
  font-size: 18px;
}

/* Содержимое узла */
.node-content {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
  min-width: 0;
}

.node-title {
  font-size: 15px;
  font-weight: 600;
  color: #0f172a;
  white-space: nowrap;
}

.node-code {
  font-family: 'JetBrains Mono', monospace;
  font-size: 12px;
  color: #64748b;
  background: rgba(0, 0, 0, 0.03);
  padding: 2px 6px;
  border-radius: 4px;
  font-weight: 500;
}

/* Статус и бейджи */
.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #10b981;
  box-shadow: 0 0 0 2px #ecfdf5;
  flex-shrink: 0;
}

.status-dot.inactive {
  background: #cbd5e1;
  box-shadow: 0 0 0 2px #f1f5f9;
}

.badge-deleted {
  font-size: 10px;
  font-weight: 700;
  color: #64748b;
  background: #f1f5f9;
  padding: 2px 8px;
  border-radius: 99px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  border: 1px solid #e2e8f0;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.node-row.deleted .node-title,
.node-row.deleted .node-price {
  color: #94a3b8;
  text-decoration: line-through;
}

.node-row.deleted .node-icon {
  color: #cbd5e1;
}

.badge-auction {
  font-size: 10px;
  font-weight: 700;
  color: #f59e0b;
  background: #fffbeb;
  padding: 2px 8px;
  border-radius: 99px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  border: 1px solid rgba(245, 158, 11, 0.2);
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

/* Цена */
.node-price {
  font-family: 'JetBrains Mono', monospace;
  font-size: 14px;
  font-weight: 700;
  color: #0f172a;
  margin-left: auto;
  padding-right: 16px;
  flex-shrink: 0;
}

/* Контекстные действия */
.node-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  opacity: 0;
  transform: translateX(10px);
  transition: all 0.2s ease-in-out;
}

.node-row:hover .node-actions {
  opacity: 1;
  transform: translateX(0);
}

.btn-action {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  border: none;
  background: transparent;
  color: #64748b;
  font-size: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
}

.btn-action:hover {
  background: #ffffff;
  color: #0f172a;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.05);
}

.btn-action.add:hover {
  color: #5c60f5;
}

.btn-action.restore:hover {
  color: #0ea5e9;
  background: #f0f9ff;
}

.btn-action.delete:hover {
  color: #ef4444;
  background: #fef2f2;
}

@media (max-width: 640px) {
  .node-row {
    flex-wrap: wrap;
    gap: 8px;
  }
  .node-actions {
    opacity: 1;
    transform: none;
    margin-left: auto;
  }
}
</style>

