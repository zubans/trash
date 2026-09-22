<template>
  <div v-if="badge || queueLines.length || showCta" class="perk-badge" :class="{ compact, active: !!badge }">
    <div v-if="badge" class="perk-line">
      <i class="ph-fill ph-lightning"></i>
      <span>{{ badge }}</span>
    </div>
    <div v-for="line in queueLines" :key="line" class="perk-next">{{ line }}</div>
    <router-link v-if="showCta && shopRoute" :to="shopRoute" class="perk-cta">
      <i class="ph-bold ph-storefront"></i>
      {{ $t('shop.perk.cta') }}
    </router-link>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, type PropType } from 'vue'
import type { UserPerk } from '../../api/shop'
import { perkBadge, perkQueueLines } from '../../utils/perk'

// Плашка привилегии: «Комиссия 7 % → 3.5 % до 17 октября» и очередь за
// ней. Стоит там, где исполнитель уже видит свою ставку, — на странице
// достижений и на дашборде (implementation_plan_shop.md §3.6).
export default defineComponent({
  name: 'PerkBadge',
  props: {
    level: {
      type: Object as PropType<{
        level_percent: number
        percent: number
        perk_rule?: string
        perk_expires_at?: string
      } | null>,
      default: null,
    },
    queue: { type: Array as PropType<UserPerk[]>, default: () => [] },
    compact: { type: Boolean, default: false },
    // Ссылка «Снизить комиссию» — когда привилегии нет, а магазин открыт.
    shopRoute: { type: String, default: '' },
    offerCta: { type: Boolean, default: false },
  },
  setup(props) {
    const badge = computed(() => (props.level ? perkBadge(props.level) : ''))
    const queueLines = computed(() => perkQueueLines(props.queue))
    const showCta = computed(() => props.offerCta && !badge.value && !queueLines.value.length)
    return { badge, queueLines, showCta }
  },
})
</script>

<style scoped>
.perk-badge {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 12px;
  padding: 10px 12px;
  border-radius: 12px;
  background: #f8fafc;
  font-size: 13px;
  color: #475569;
}
.perk-badge.active {
  background: #ecfdf5;
  color: #065f46;
}
.perk-badge.compact {
  margin-top: 8px;
  padding: 8px 10px;
}
.perk-line {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
}
.perk-next {
  padding-left: 20px;
  color: #64748b;
}
.perk-cta {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: #4f46e5;
  font-weight: 600;
  text-decoration: none;
}
</style>
