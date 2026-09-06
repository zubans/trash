<template>
  <div class="achievements-wrapper">
    <div class="achievements-container">
      <div class="top-nav">
        <button type="button" class="btn-back" @click="goBack">
          <i class="ph-bold ph-arrow-left"></i>
          Вернуться на главную
        </button>
        <div class="page-header-title">
          <i class="ph-fill ph-trophy icon-title"></i>
          Достижения
        </div>
      </div>

      <!-- Уровень: главное, что тут есть. Значки — следствие, а ставка комиссии
           это то, ради чего их собирают. -->
      <div class="level-card">
        <div class="level-head">
          <div class="level-badge">{{ level.level }}</div>
          <div class="level-text">
            <div class="level-title">Уровень {{ level.level }}</div>
            <div class="level-sub">{{ level.points }} {{ pointWord(level.points) }}</div>
          </div>
          <div class="level-commission">
            <div class="commission-value">{{ formatPercent(level.percent) }}%</div>
            <div class="commission-label">комиссия</div>
            <div v-if="level.discount_pp > 0" class="commission-was">
              вместо {{ formatPercent(level.base_percent) }}%
            </div>
          </div>
        </div>

        <div v-if="level.next_level_points > 0" class="level-progress">
          <div class="progress-track">
            <div class="progress-fill" :style="{ width: levelProgress + '%' }"></div>
          </div>
          <div class="progress-caption">
            До {{ level.level + 1 }} уровня — {{ level.next_level_points - level.points }}
            {{ pointWord(level.next_level_points - level.points) }}. Он снизит комиссию ещё на
            {{ formatPercent(stepPP) }}%.
          </div>
        </div>
        <div v-else-if="level.base_percent > 0" class="level-progress">
          <div class="progress-caption">
            Комиссия уже нулевая — дальше ачивки приносят значки и подарки, но не скидку.
          </div>
        </div>

        <!-- Истекающие баллы показываются заранее и намеренно: уровень
             считается по действующим баллам, поэтому истечение его снижает. -->
        <div v-if="expiring.length" class="level-warning">
          <i class="ph-fill ph-clock-countdown"></i>
          <span>
            {{ expiringPoints }} {{ pointWord(expiringPoints) }} сгорят до
            {{ formatDate(expiring[0].expires_at) }} — уровень может снизиться.
          </span>
        </div>
      </div>

      <div v-if="loading" class="state-note">Загружаем достижения…</div>
      <div v-else-if="error" class="state-note error">{{ error }}</div>
      <div v-else-if="!cards.length" class="state-note">
        Пока ни одной доступной ачивки. Загляните позже — их включает администратор.
      </div>

      <div v-else class="cards-grid">
        <div
          v-for="card in cards"
          :key="card.code"
          class="achievement-card"
          :class="{ granted: card.granted }"
        >
          <div class="card-icon"><i :class="iconClass(card)"></i></div>
          <div class="card-body">
            <div class="card-title">
              {{ card.title }}
              <span v-if="card.count > 1" class="card-count">×{{ card.count }}</span>
            </div>
            <div class="card-description">{{ card.description }}</div>

            <div class="card-meta">
              <span class="chip points">+{{ card.weight }} {{ pointWord(card.weight) }}</span>
              <span v-if="card.repeatable" class="chip">повторяемая</span>
              <span v-if="card.available_to" class="chip warn">
                до {{ formatDate(card.available_to) }}
              </span>
            </div>

            <div v-if="card.granted" class="card-granted">
              <i class="ph-fill ph-check-circle"></i>
              Получено {{ formatDate(card.granted_at) }}
              <template v-if="card.points"> · {{ card.points }} {{ pointWord(card.points) }} в зачёт</template>
              <template v-if="card.expires_at"> · сгорает {{ formatDate(card.expires_at) }}</template>
            </div>
            <div v-else-if="card.progress !== undefined" class="card-progress">
              <div class="progress-track slim">
                <div class="progress-fill" :style="{ width: Math.round(card.progress * 100) + '%' }"></div>
              </div>
              <span>{{ Math.round(card.progress * 100) }}%</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import {
  getAchievements,
  getLevel,
  type AchievementCard,
  type ExecutorLevel,
} from '../../api/achievements'

// Иконки скрипта — короткие имена вроде "revolver": сопоставление с набором
// Phosphor живёт здесь, чтобы скрипт не знал ничего о том, чем его рисуют.
const ICONS: Record<string, string> = {
  trophy: 'ph-fill ph-trophy',
  revolver: 'ph-fill ph-crosshair',
  medal: 'ph-fill ph-medal',
  star: 'ph-fill ph-star',
  fire: 'ph-fill ph-fire',
}

export default defineComponent({
  name: 'AchievementsPage',
  setup() {
    const router = useRouter()

    const cards = ref<AchievementCard[]>([])
    const level = ref<ExecutorLevel>({
      points: 0,
      level: 0,
      next_level_points: 0,
      base_percent: 0,
      discount_pp: 0,
      percent: 0,
      max_useful_level: 0,
    })
    const loading = ref(true)
    const error = ref('')

    const load = async () => {
      loading.value = true
      error.value = ''
      try {
        const [list, current] = await Promise.all([getAchievements(), getLevel()])
        cards.value = list
        level.value = current
      } catch {
        error.value = 'Не удалось загрузить достижения. Попробуйте обновить страницу.'
      } finally {
        loading.value = false
      }
    }

    // Шаг скидки выводится из уже известных чисел, а не запрашивается отдельно:
    // discount_pp / level — это и есть цена уровня в процентных пунктах.
    const stepPP = computed(() => {
      if (level.value.level > 0) return level.value.discount_pp / level.value.level
      if (level.value.max_useful_level > 0) return level.value.base_percent / level.value.max_useful_level
      return 1
    })

    // Полоса рисуется внутри текущего уровня, а не от нуля: иначе на десятом
    // уровне она всегда почти полная и ничего не показывает.
    const levelProgress = computed(() => {
      const next = level.value.next_level_points
      if (next <= 0) return 100
      const perLevel = next / (level.value.level + 1)
      const inLevel = level.value.points - perLevel * level.value.level
      return Math.max(0, Math.min(100, Math.round((inLevel / perLevel) * 100)))
    })

    const expiring = computed(() =>
      cards.value
        .filter((card) => card.granted && card.expires_at)
        .sort((a, b) => String(a.expires_at).localeCompare(String(b.expires_at))),
    )
    const expiringPoints = computed(() =>
      expiring.value.reduce((sum, card) => sum + (card.points || 0), 0),
    )

    const pointWord = (value: number) => {
      const abs = Math.abs(value)
      const last = abs % 10
      const lastTwo = abs % 100
      if (lastTwo >= 11 && lastTwo <= 19) return 'баллов'
      if (last === 1) return 'балл'
      if (last >= 2 && last <= 4) return 'балла'
      return 'баллов'
    }

    const formatDate = (value?: string) => {
      if (!value) return ''
      return new Date(value).toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' })
    }

    // Ставка приходит числом с плавающей точкой; дробные проценты бывают, но
    // «10.00%» на карточке — шум.
    const formatPercent = (value: number) =>
      Number.isInteger(value) ? String(value) : value.toFixed(1)

    const iconClass = (card: AchievementCard) => ICONS[card.icon] ?? 'ph-fill ph-seal-check'

    const goBack = () => router.push('/executor')

    onMounted(load)

    return {
      cards,
      level,
      loading,
      error,
      stepPP,
      levelProgress,
      expiring,
      expiringPoints,
      pointWord,
      formatDate,
      formatPercent,
      iconClass,
      goBack,
    }
  },
})
</script>

<style scoped>
.achievements-wrapper {
  min-height: 100vh;
  background: #f6f7fb;
  padding: 16px;
}

.achievements-container {
  max-width: 880px;
  margin: 0 auto;
}

.top-nav {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.btn-back {
  border: none;
  background: #fff;
  border-radius: 10px;
  padding: 8px 14px;
  font-size: 14px;
  color: #4b5563;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.page-header-title {
  font-size: 18px;
  font-weight: 600;
  color: #111827;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.icon-title {
  color: #f59e0b;
}

.level-card {
  background: linear-gradient(135deg, #1f2937, #111827);
  color: #fff;
  border-radius: 16px;
  padding: 20px;
  margin-bottom: 20px;
}

.level-head {
  display: flex;
  align-items: center;
  gap: 16px;
}

.level-badge {
  width: 56px;
  height: 56px;
  border-radius: 50%;
  background: #f59e0b;
  color: #111827;
  font-size: 24px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.level-text {
  flex: 1;
  min-width: 0;
}

.level-title {
  font-size: 18px;
  font-weight: 600;
}

.level-sub {
  font-size: 13px;
  opacity: 0.75;
}

.level-commission {
  text-align: right;
}

.commission-value {
  font-size: 24px;
  font-weight: 700;
  color: #34d399;
}

.commission-label {
  font-size: 12px;
  opacity: 0.7;
}

.commission-was {
  font-size: 12px;
  opacity: 0.6;
  text-decoration: line-through;
}

.level-progress {
  margin-top: 16px;
}

.progress-track {
  height: 8px;
  background: rgba(255, 255, 255, 0.15);
  border-radius: 999px;
  overflow: hidden;
}

.progress-track.slim {
  height: 6px;
  background: #e5e7eb;
  flex: 1;
}

.progress-fill {
  height: 100%;
  background: #34d399;
  border-radius: 999px;
  transition: width 0.3s ease;
}

.progress-caption {
  font-size: 12px;
  opacity: 0.8;
  margin-top: 8px;
}

.level-warning {
  margin-top: 14px;
  padding: 10px 12px;
  border-radius: 10px;
  background: rgba(251, 191, 36, 0.15);
  color: #fcd34d;
  font-size: 13px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.state-note {
  background: #fff;
  border-radius: 12px;
  padding: 20px;
  text-align: center;
  color: #6b7280;
  font-size: 14px;
}

.state-note.error {
  color: #b91c1c;
}

.cards-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 12px;
}

.achievement-card {
  background: #fff;
  border-radius: 14px;
  padding: 16px;
  display: flex;
  gap: 14px;
  border: 1px solid #eef0f4;
}

.achievement-card.granted {
  border-color: #34d399;
}

.card-icon {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: #f3f4f6;
  color: #9ca3af;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 22px;
  flex-shrink: 0;
}

.achievement-card.granted .card-icon {
  background: #ecfdf5;
  color: #059669;
}

.card-body {
  min-width: 0;
  flex: 1;
}

.card-title {
  font-weight: 600;
  color: #111827;
  font-size: 15px;
}

.card-count {
  color: #059669;
  font-size: 13px;
  margin-left: 4px;
}

.card-description {
  color: #6b7280;
  font-size: 13px;
  margin-top: 4px;
}

.card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
}

.chip {
  font-size: 11px;
  padding: 3px 8px;
  border-radius: 999px;
  background: #f3f4f6;
  color: #4b5563;
}

.chip.points {
  background: #eff6ff;
  color: #1d4ed8;
}

.chip.warn {
  background: #fef3c7;
  color: #92400e;
}

.card-granted {
  margin-top: 10px;
  font-size: 12px;
  color: #059669;
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.card-progress {
  margin-top: 10px;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: #6b7280;
}

@media (max-width: 600px) {
  .level-head {
    flex-wrap: wrap;
  }

  .level-commission {
    text-align: left;
  }
}
</style>
