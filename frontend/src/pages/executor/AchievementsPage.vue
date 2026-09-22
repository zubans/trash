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

        <!-- Привилегия магазина — рядом со ставкой по уровню, к которой она
             применяется: «7 % × 0.5 = 3.5 % до 17 октября» и очередь за ней. -->
        <PerkBadge :level="level" :queue="level.perk_queue || []" />

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

      <div v-else-if="!shelf.length && !locked.length" class="state-note">
        Пока ни одной доступной ачивки. Загляните позже — их включает администратор.
      </div>

      <template v-else>
        <!-- Полка. Заслуженное лежит здесь и остаётся здесь: значок не исчезает
             оттого, что акцию закрыли, ачивку выключили или её правило убрали. -->
        <section class="section">
          <div class="section-head">
            <h2 class="section-title">Полка достижений</h2>
            <span class="section-count">{{ shelf.length }}</span>
          </div>

          <div v-if="shelf.length" class="shelf">
            <div v-for="card in shelf" :key="card.code" class="shelf-item">
              <div class="shelf-figure">
                <PixelAchievementIcon :name="card.icon" :size="56" :title="card.title" />
                <span v-if="card.count > 1" class="shelf-count">×{{ card.count }}</span>
              </div>
              <div class="shelf-title">{{ card.title }}</div>
              <div class="shelf-note">{{ formatDate(card.granted_at) }}</div>
              <div v-if="card.points" class="shelf-points">
                +{{ card.points }} {{ pointWord(card.points) }}
              </div>
              <div v-if="card.expires_at" class="shelf-note warn">
                баллы сгорят {{ formatDate(card.expires_at) }}
              </div>
              <!-- Повторяемую ачивку её скрипт продолжает считать и после
                   выдачи. Что именно значит полоса, решает он же — у одной это
                   путь к следующей выдаче, у другой близость к попаданию, —
                   поэтому подпись нейтральная, а не выдуманная экраном. -->
              <div v-if="card.repeatable && card.progress !== undefined" class="shelf-progress">
                <div class="progress-track slim">
                  <div class="progress-fill" :style="{ width: Math.round(card.progress * 100) + '%' }"></div>
                </div>
                <span class="shelf-note">прогресс</span>
              </div>
              <div v-else-if="!card.available" class="shelf-note">акция завершена</div>
            </div>
          </div>
          <div v-else class="state-note">
            Полка пока пуста. Первый значок появится здесь сразу после того, как заказчик
            подтвердит выполненный заказ.
          </div>
        </section>

        <!-- Витрина: то, что ещё можно заслужить. -->
        <section v-if="locked.length" class="section">
          <div class="section-head">
            <h2 class="section-title">Ещё не получены</h2>
            <span class="section-count">{{ locked.length }}</span>
          </div>

          <div class="cards-grid">
            <div v-for="card in locked" :key="card.code" class="achievement-card">
              <div class="card-icon">
                <PixelAchievementIcon :name="card.icon" :size="40" locked :title="card.title" />
              </div>
              <div class="card-body">
                <div class="card-title">{{ card.title }}</div>
                <div class="card-description">{{ card.description }}</div>

                <div class="card-meta">
                  <span class="chip points">+{{ card.weight }} {{ pointWord(card.weight) }}</span>
                  <span v-if="card.repeatable" class="chip">повторяемая</span>
                  <span v-if="card.available_to" class="chip warn">
                    до {{ formatDate(card.available_to) }}
                  </span>
                </div>

                <div v-if="card.progress !== undefined" class="card-progress">
                  <div class="progress-track slim">
                    <div class="progress-fill" :style="{ width: Math.round(card.progress * 100) + '%' }"></div>
                  </div>
                  <span>{{ Math.round(card.progress * 100) }}%</span>
                </div>
              </div>
            </div>
          </div>
        </section>
      </template>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import PixelAchievementIcon from '../../components/PixelAchievementIcon.vue'
import PerkBadge from '../../components/shop/PerkBadge.vue'
import {
  getAchievements,
  getLevel,
  type AchievementCard,
  type ExecutorLevel,
} from '../../api/achievements'

export default defineComponent({
  name: 'AchievementsPage',
  components: { PixelAchievementIcon, PerkBadge },
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
      level_percent: 0,
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

    // Полка и витрина — один список, разделённый по одному признаку: значок
    // либо заслужен, либо нет. Порядок внутри приходит с сервера (sort_order),
    // и переставлять его здесь не за чем.
    const shelf = computed(() => cards.value.filter((card) => card.granted))
    const locked = computed(() => cards.value.filter((card) => !card.granted))

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

    const goBack = () => router.push('/executor')

    onMounted(load)

    return {
      cards,
      shelf,
      locked,
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

.section {
  margin-bottom: 24px;
}

.section-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}

.section-title {
  font-size: 15px;
  font-weight: 600;
  color: #111827;
  margin: 0;
}

.section-count {
  font-size: 12px;
  font-weight: 600;
  color: #6b7280;
  background: #e5e7eb;
  border-radius: 999px;
  padding: 1px 8px;
}

/* Полка. Значки стоят в ячейках одной высоты, и нижняя грань ячейки — это доска:
   у соседей по строке грани сходятся, поэтому ряд читается полкой, а не списком
   карточек, при любом числе колонок. */
.shelf {
  background: #fff;
  border-radius: 14px;
  padding: 12px 12px 0;
  border: 1px solid #eef0f4;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(104px, 1fr));
}

.shelf-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 10px 6px 14px;
  margin-bottom: 12px;
  border-bottom: 4px solid #d9c3a1;
  border-radius: 2px;
  min-width: 0;
}

.shelf-figure {
  position: relative;
  /* Значок стоит на доске, а не парит над ней. */
  margin-bottom: 8px;
}

.shelf-count {
  position: absolute;
  right: -6px;
  bottom: -2px;
  background: #059669;
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  border-radius: 999px;
  padding: 1px 6px;
}

.shelf-title {
  font-size: 12px;
  font-weight: 600;
  color: #111827;
  line-height: 1.25;
}

.shelf-note {
  font-size: 11px;
  color: #9ca3af;
  margin-top: 2px;
}

.shelf-note.warn {
  color: #b45309;
}

.shelf-points {
  font-size: 11px;
  font-weight: 600;
  color: #1d4ed8;
  margin-top: 2px;
}

.shelf-progress {
  width: 100%;
  margin-top: 6px;
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

.card-icon {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: #f3f4f6;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
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
