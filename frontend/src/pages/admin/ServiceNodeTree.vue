<template>
  <div class="node-tree-wrap">
    <div v-for="item in nodes" :key="item.node.id" class="node-card mb-3">
      <!-- Node Header Row -->
      <div class="node-main-row">
        <div class="node-info">
          <!-- Icon -->
          <div class="node-icon-box" :class="item.node.node_type === 'CATEGORY' ? 'is-cat' : 'is-var'">
            <i :class="item.node.node_type === 'CATEGORY' ? 'ph-fill ph-folder' : 'ph-fill ph-tag'"></i>
          </div>

          <div>
            <div class="node-title-row">
              <span class="node-name">{{ item.node.name['ru'] || item.node.code }}</span>
              <span class="node-code">({{ item.node.code }})</span>
              <span v-if="item.node.name['en']" class="node-name-en">{{ item.node.name['en'] }}</span>
            </div>

            <div class="node-meta-row">
              <span class="badge-type" :class="item.node.node_type === 'CATEGORY' ? 'cat' : 'var'">
                {{ item.node.node_type === 'CATEGORY' ? 'Категория' : 'Вариант' }}
              </span>

              <span v-if="item.node.base_price" class="badge-meta price-badge">
                <i class="ph-bold ph-currency-rub me-1"></i> {{ item.node.base_price }} ₽
              </span>

              <span v-if="item.node.is_auction" class="badge-meta auction-badge">
                <i class="ph-bold ph-gavel me-1"></i> Аукцион
              </span>

              <span v-if="item.node.is_active" class="badge-meta active-badge">
                <i class="ph-fill ph-check-circle me-1"></i> Активен
              </span>
              <span v-else class="badge-meta inactive-badge">
                <i class="ph-bold ph-prohibit me-1"></i> Отключен
              </span>

              <span class="sort-order-text">Сортировка: {{ item.node.sort_order || 1 }}</span>
            </div>
          </div>
        </div>

        <!-- Node Actions -->
        <div class="node-actions">
          <button
            v-if="item.node.node_type === 'CATEGORY'"
            type="button"
            class="btn-node-act add-child"
            title="Добавить в подкатегорию"
            @click="$emit('create', item.node.id)"
          >
            <i class="ph-bold ph-plus me-1"></i> Добавить подэлемент
          </button>

          <button
            type="button"
            class="btn-node-act edit"
            title="Редактировать"
            @click="$emit('edit', item.node)"
          >
            <i class="ph-bold ph-pencil me-1"></i> Изменить
          </button>

          <button
            type="button"
            class="btn-node-act delete"
            title="Удалить"
            @click="$emit('delete', item.node)"
          >
            <i class="ph-bold ph-trash"></i>
          </button>
        </div>
      </div>

      <!-- Nested Children -->
      <div v-if="item.children && item.children.length > 0" class="node-children-wrap">
        <service-node-tree
          :nodes="item.children"
          @create="$emit('create', $event)"
          @edit="$emit('edit', $event)"
          @delete="$emit('delete', $event)"
        />
      </div>
    </div>
  </div>
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
.node-tree-wrap {
  width: 100%;
}

.node-card {
  background: #ffffff;
  border-radius: 16px;
  border: 1px solid #e2e8f0;
  box-shadow: 0 2px 8px rgba(15, 23, 42, 0.04);
  overflow: hidden;
  transition: all 0.2s ease;
}

.node-card:hover {
  border-color: #cbd5e1;
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.07);
}

.node-main-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  gap: 16px;
}

.node-info {
  display: flex;
  align-items: center;
  gap: 14px;
}

.node-icon-box {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  flex-shrink: 0;
}

.node-icon-box.is-cat {
  background: rgba(99, 102, 241, 0.1);
  color: #6366f1;
}

.node-icon-box.is-var {
  background: rgba(16, 185, 129, 0.1);
  color: #10b981;
}

.node-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.node-name {
  font-size: 15px;
  font-weight: 700;
  color: #0f172a;
}

.node-code {
  font-size: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  color: #64748b;
}

.node-name-en {
  font-size: 13px;
  color: #94a3b8;
  font-style: italic;
}

.node-meta-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 4px;
  flex-wrap: wrap;
}

.badge-type {
  font-size: 11px;
  font-weight: 700;
  padding: 2px 8px;
  border-radius: 6px;
  text-transform: uppercase;
  letter-spacing: 0.3px;
}

.badge-type.cat {
  background: #e0e7ff;
  color: #3730a3;
}

.badge-type.var {
  background: #d1fae5;
  color: #065f46;
}

.badge-meta {
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
}

.price-badge {
  background: #fef3c7;
  color: #92400e;
}

.auction-badge {
  background: #fae8ff;
  color: #86198f;
}

.active-badge {
  background: #ecfdf5;
  color: #10b981;
}

.inactive-badge {
  background: #fef2f2;
  color: #ef4444;
}

.sort-order-text {
  font-size: 11px;
  color: #94a3b8;
}

/* Actions */
.node-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.btn-node-act {
  padding: 7px 12px;
  border-radius: 8px;
  font-size: 12px;
  font-weight: 600;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  color: #475569;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  transition: all 0.2s ease;
}

.btn-node-act:hover {
  background: #f8fafc;
  color: #0f172a;
}

.btn-node-act.add-child {
  background: rgba(99, 102, 241, 0.08);
  color: #6366f1;
  border-color: rgba(99, 102, 241, 0.2);
}

.btn-node-act.add-child:hover {
  background: #6366f1;
  color: #ffffff;
}

.btn-node-act.edit:hover {
  background: #f59e0b;
  color: #ffffff;
  border-color: #f59e0b;
}

.btn-node-act.delete:hover {
  background: #ef4444;
  color: #ffffff;
  border-color: #ef4444;
}

.node-children-wrap {
  padding-left: 28px;
  padding-right: 16px;
  padding-bottom: 16px;
  border-top: 1px solid #f1f5f9;
  background: #fafbfd;
  padding-top: 14px;
}
</style>
