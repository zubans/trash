<template>
  <va-modal
    :model-value="modelValue"
    :title="title"
    size="large"
    hide-default-actions
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <div class="history">
      <p class="history-sub">
        {{ user?.phone }}
        <span v-if="fullName"> · {{ fullName }}</span>
      </p>

      <!-- Две вкладки в одном окне: администратор, открывший проводки, почти
           всегда следом смотрит заказы, и закрывать окно ради этого незачем. -->
      <div class="tabs">
        <button
          type="button"
          class="tab"
          :class="{ active: tab === 'transactions' }"
          @click="switchTo('transactions')"
        >
          Проводки<span v-if="tab === 'transactions' && total"> · {{ total }}</span>
        </button>
        <button
          type="button"
          class="tab"
          :class="{ active: tab === 'orders' }"
          @click="switchTo('orders')"
        >
          Заказы<span v-if="tab === 'orders' && total"> · {{ total }}</span>
        </button>
        <button
          v-if="canSeeAchievements"
          type="button"
          class="tab"
          :class="{ active: tab === 'achievements' }"
          @click="switchTo('achievements')"
        >
          Ачивки<span v-if="tab === 'achievements' && total"> · {{ total }}</span>
        </button>
        <button
          v-if="canSeePenalties"
          type="button"
          class="tab"
          :class="{ active: tab === 'penalties' }"
          @click="switchTo('penalties')"
        >
          Штрафы
        </button>
      </div>

      <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>
      <p v-if="tab === 'orders' && actionMsg" class="alert success">{{ actionMsg }}</p>

      <div v-if="tab === 'transactions' || tab === 'orders'" class="table-scroll">
        <table v-if="tab === 'transactions'" class="history-table">
          <thead>
            <tr>
              <th>Дата</th>
              <th>Тип</th>
              <th class="num">Сумма</th>
              <th>Заказ</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="tx in transactions" :key="tx.id">
              <td class="nowrap">{{ formatDate(tx.created_at) }}</td>
              <td>{{ typeLabel(tx.type) }}</td>
              <!-- Знак берётся из direction, посчитанного на сервере: суммы в
                   таблице все положительные, направление живёт в типе. -->
              <td class="num" :class="signClass(tx.direction)">
                {{ formatSigned(tx.amount, tx.direction) }}
              </td>
              <td class="mono">{{ tx.order_id ? tx.order_id.slice(0, 8) : '—' }}</td>
            </tr>
            <tr v-if="!loading && !transactions.length">
              <td colspan="4" class="empty">Проводок нет.</td>
            </tr>
          </tbody>
        </table>

        <table v-else-if="tab === 'orders'" class="history-table">
          <thead>
            <tr>
              <th>Дата</th>
              <th>Услуга</th>
              <th>Роль</th>
              <th>Статус</th>
              <th class="num">Сумма</th>
              <th v-if="canEditOrders"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="order in orders" :key="order.id">
              <td class="nowrap">{{ formatDate(order.created_at) }}</td>
              <td>
                {{ order.service_variant_name }}
                <div v-if="order.address" class="muted">{{ order.address }}</div>
              </td>
              <td>{{ roleIn(order) }}</td>
              <td>{{ statusLabel(order.status) }}</td>
              <td class="num">{{ formatAmount(order.final_amount ?? order.hold_amount) }}</td>
              <td v-if="canEditOrders" class="nowrap">
                <button
                  v-if="canReturnToWork(order.status)"
                  type="button"
                  class="btn-action"
                  :disabled="busy"
                  @click="returnToWork(order)"
                >
                  <i class="ph-bold ph-arrow-counter-clockwise"></i>
                  Вернуть в работу
                </button>
              </td>
            </tr>
            <tr v-if="!loading && !orders.length">
              <td :colspan="canEditOrders ? 6 : 5" class="empty">Заказов нет.</td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Ачивки. Две кнопки закрывают две разные ситуации, и путать их не
           нужно: пересчёт повторяет правило по истории, ручная выдача правило
           обходит. -->
      <div v-if="tab === 'achievements'" class="achievements">
        <div class="level-line">
          <span class="level-badge">{{ level?.level ?? 0 }}</span>
          <span>
            уровень · {{ level?.points ?? 0 }} действующих балл(ов) · комиссия
            {{ level?.percent ?? 0 }}%
          </span>
        </div>

        <div class="ach-actions">
          <button type="button" class="btn-action" :disabled="busy" @click="recheck">
            <i class="ph-bold ph-arrows-clockwise"></i>
            Пересчитать условия
          </button>
          <button type="button" class="btn-action" :disabled="busy" @click="recalcStats">
            <i class="ph-bold ph-calculator"></i>
            Пересчитать агрегаты
          </button>
        </div>
        <p class="hint">
          Пересчёт повторяет подтверждённые заказы и выдаёт то, что выдало бы
          правило: он нужен, когда ачивку включили после того, как человек уже
          отработал. Незаслуженного он не выдаёт.
        </p>

        <div v-if="grantable.length" class="ach-grant">
          <select v-model="grantCode" class="grant-select">
            <option value="">— выдать вручную —</option>
            <option v-for="item in grantable" :key="item.code" :value="item.code">
              {{ item.title || item.code }} · {{ item.effective_weight }} б.
            </option>
          </select>
          <input v-model="grantReason" class="grant-reason" placeholder="причина (в аудит)" />
          <button
            type="button"
            class="btn-action primary"
            :disabled="busy || !grantCode"
            @click="grant"
          >
            Выдать
          </button>
        </div>

        <p v-if="actionMsg" class="alert success">{{ actionMsg }}</p>

        <!-- Вес выдачи и начисленные по ней баллы — разные числа, и расходятся
             они законно: суточный потолок ужимает начисление, а выдача помнит,
             сколько ачивка стоила. Уровень считается по начисленному. -->
        <p v-if="weightSum !== (level?.points ?? 0)" class="hint">
          Сумма весов — {{ weightSum }}, действующих баллов — {{ level?.points ?? 0 }}.
          Разницу съедает суточный потолок начисления или истёкший срок баллов.
        </p>

        <table class="history-table">
          <thead>
            <tr>
              <th>Дата</th>
              <th>Ачивка</th>
              <th class="num">Вес</th>
              <th>Состояние</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="g in grants" :key="g.id" :class="{ revoked: g.revoked_at }">
              <td class="nowrap">{{ formatDate(g.granted_at) }}</td>
              <td>
                {{ achievementTitle(g.code) }}
                <div class="muted mono">{{ g.code }}</div>
              </td>
              <td class="num">{{ g.points }}</td>
              <td>
                <template v-if="g.revoked_at">
                  отозвана {{ formatDate(g.revoked_at) }}
                  <div v-if="g.revoke_reason" class="muted">{{ g.revoke_reason }}</div>
                </template>
                <template v-else-if="g.expires_at">
                  баллы до {{ formatDate(g.expires_at) }}
                </template>
                <template v-else>действует</template>
              </td>
              <td>
                <button
                  v-if="!g.revoked_at && canRevoke"
                  type="button"
                  class="btn-link danger"
                  :disabled="busy"
                  @click="revoke(g)"
                >
                  Отозвать
                </button>
              </td>
            </tr>
            <tr v-if="!loading && !grants.length">
              <td colspan="5" class="empty">Ачивок нет.</td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Штрафные баллы. Журнал целиком: отменённые и сгоревшие остаются
           строками — по ним видно, за что человек наказывался раньше. -->
      <div v-if="tab === 'penalties'" class="penalties">
        <p v-if="actionMsg" class="alert success">{{ actionMsg }}</p>
        <template v-if="penalties">
          <div class="penalty-roles">
            <div v-for="role in ['EXECUTOR', 'CUSTOMER']" :key="role" class="penalty-role">
              <div class="penalty-role-title">{{ role === 'EXECUTOR' ? 'Исполнитель' : 'Заказчик' }}</div>
              <template v-if="statusFor(role)">
                <div>Действующих баллов: <strong>{{ statusFor(role)!.active_points }}</strong> из порога {{ penalties.limits.penalty_points_threshold }}</div>
                <div v-if="activeUntil(statusFor(role)!.photo_required_until)">
                  Фото-подтверждение до {{ formatDate(statusFor(role)!.photo_required_until!) }}
                </div>
                <div v-if="activeUntil(statusFor(role)!.silent_block_ends_at)" class="danger-text">
                  Тихая блокировка до {{ formatDate(statusFor(role)!.silent_block_ends_at!) }}
                </div>
              </template>
              <div v-else class="muted">Баллов не было</div>
            </div>
          </div>

          <div class="penalty-flags">
            <div v-if="penalties.flags.soft_banned_at" class="danger-text">
              Мягкий бан с {{ formatDate(penalties.flags.soft_banned_at) }}:
              {{ penalties.flags.soft_ban_reason || 'без причины' }}
              ({{ penalties.flags.soft_banned_by ? 'администратор' : 'система, рецидив' }})
            </div>
            <div v-if="penalties.flags.had_silent_block_at">
              Тихая блокировка была снята {{ formatDate(penalties.flags.had_silent_block_at) }}:
              следующий балл переведёт аккаунт в мягкий бан.
              <button
                v-if="canEditPenalties"
                type="button"
                class="btn-link"
                :disabled="busy"
                @click="resetFlag"
              >
                Сбросить флаг
              </button>
            </div>
          </div>

          <table class="history-table">
            <thead>
              <tr>
                <th>Дата</th>
                <th>Роль</th>
                <th>За что</th>
                <th>Состояние</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="point in penalties.points" :key="point.id" :class="{ revoked: point.revoked_at || point.expired_at }">
                <td class="nowrap">{{ formatDate(point.created_at) }}</td>
                <td>{{ point.role === 'EXECUTOR' ? 'исполнитель' : 'заказчик' }}</td>
                <td>
                  {{ point.reason || '—' }}
                  <div v-if="point.order_id" class="muted mono">заказ {{ point.order_id.slice(0, 8) }}</div>
                </td>
                <td>
                  <template v-if="point.revoked_at">отменён {{ formatDate(point.revoked_at) }}</template>
                  <template v-else-if="point.expired_at">сгорел {{ formatDate(point.expired_at) }}</template>
                  <template v-else>действует</template>
                </td>
                <td>
                  <button
                    v-if="!point.revoked_at && !point.expired_at && canEditPenalties"
                    type="button"
                    class="btn-link danger"
                    :disabled="busy"
                    @click="revokePoint(point)"
                  >
                    Отменить
                  </button>
                </td>
              </tr>
              <tr v-if="!penalties.points.length">
                <td colspan="5" class="empty">Штрафных баллов нет.</td>
              </tr>
            </tbody>
          </table>
        </template>
      </div>

      <div class="history-foot">
        <span class="muted">
          <template v-if="total">Показано {{ shown }} из {{ total }}</template>
        </span>
        <div class="foot-actions">
          <button
            v-if="(tab === 'transactions' || tab === 'orders') && shown < total"
            type="button"
            class="btn-more"
            :disabled="loading"
            @click="loadMore"
          >
            Показать ещё
          </button>
          <button type="button" class="btn-close" @click="$emit('update:modelValue', false)">
            Закрыть
          </button>
        </div>
      </div>
    </div>
  </va-modal>
</template>

<script lang="ts">
import { computed, defineComponent, PropType, ref, watch } from 'vue'
import {
  getUserOrders,
  getUserTransactions,
  type UserOrder,
  type UserTransaction,
} from '../../api/user-history'
import {
  adminGetAchievements,
  adminGetUserAchievements,
  adminGrantAchievement,
  adminRecalculateStats,
  adminRecheckUserAchievements,
  adminRevokeGrant,
  type AdminAchievement,
  type ExecutorLevel,
  type UserGrant,
} from '../../api/achievements'
import { useAuthStore } from '../../stores/auth-store'
import {
  ORDER_STATUS_LABELS,
  RETURN_TO_WORK_CONFIRM,
  canReturnToWork,
  returnOrderToWork,
} from '../../api/admin-orders'
import {
  getUserPenalties,
  resetSilentBlockFlag,
  revokePenaltyPoint,
  type AdminPenaltyView,
  type PenaltyPoint,
} from '../../api/penalties'

const PAGE_SIZE = 20

const TYPE_LABELS: Record<string, string> = {
  TOP_UP: 'Пополнение',
  WITHDRAWAL: 'Вывод',
  WITHDRAWAL_HOLD: 'Вывод: удержание',
  WITHDRAWAL_PAID: 'Вывод: выплата',
  HOLD: 'Удержание по заказу',
  PAYMENT: 'Оплата заказа',
  REWARD: 'Вознаграждение',
  REFUND: 'Возврат',
  FINE: 'Штраф',
  TIP: 'Чаевые',
  TIP_REWARD: 'Чаевые исполнителю',
  COMMISSION: 'Комиссия платформы',
  BONUS: 'Бонус',
  DISPUTE_REWARD: 'Оплата по спору',
}

type Tab = 'transactions' | 'orders' | 'achievements' | 'penalties'


export default defineComponent({
  name: 'UserHistoryModal',
  props: {
    modelValue: { type: Boolean, default: false },
    user: { type: Object as PropType<any | null>, default: null },
    // С какой вкладки открыть: пункты меню на карточке ведут каждый на свою.
    initialTab: {
      type: String as PropType<Tab>,
      default: 'transactions',
    },
  },
  emits: ['update:modelValue'],
  setup(props) {
    const authStore = useAuthStore()
    const tab = ref<Tab>(props.initialTab)
    const transactions = ref<UserTransaction[]>([])
    const orders = ref<UserOrder[]>([])
    const total = ref(0)
    const loading = ref(false)
    const errorMsg = ref('')

    // Ачивки. Каталог нужен ради двух вещей: названий в таблице выдач (в самой
    // выдаче лежит только код) и списка того, что вообще можно выдать вручную.
    const grants = ref<UserGrant[]>([])
    const level = ref<ExecutorLevel | null>(null)
    const catalog = ref<AdminAchievement[]>([])
    const grantCode = ref('')
    const grantReason = ref('')
    const busy = ref(false)
    const actionMsg = ref('')

    const canSeeAchievements = computed(() => authStore.can('achievements.view'))
    const canGrant = computed(() => authStore.can('achievements.create'))
    const canRevoke = computed(() => authStore.can('achievements.delete'))

    // Штрафы видит тот, кто видит пользователей; менять — право penalties.edit.
    const penalties = ref<AdminPenaltyView | null>(null)
    const canSeePenalties = computed(() => authStore.can('users.view'))
    const canEditPenalties = computed(() => authStore.can('penalties.edit'))
    const canEditOrders = computed(() => authStore.can('orders.edit'))

    // Выдать вручную можно только включённую ачивку с загруженным скриптом —
    // ровно то, что разрешает сервер. Предлагать в списке большее значило бы
    // обещать кнопку, которая ответит отказом.
    const grantable = computed(() =>
      canGrant.value
        ? catalog.value.filter((item) => item.is_active && item.script_loaded && !item.deleted_at)
        : [],
    )

    const weightSum = computed(() =>
      grants.value.reduce((sum, g) => (g.revoked_at ? sum : sum + (g.points || 0)), 0),
    )

    const shown = computed(() =>
      tab.value === 'transactions' ? transactions.value.length : orders.value.length,
    )

    const fullName = computed(() => {
      const u = props.user
      if (!u) return ''
      return [u.last_name, u.first_name, u.patronymic].filter(Boolean).join(' ')
    })

    const TITLES: Record<Tab, string> = {
      transactions: 'История проводок',
      orders: 'История заказов',
      achievements: 'Ачивки пользователя',
      penalties: 'Штрафные баллы',
    }
    const title = computed(() => TITLES[tab.value])

    const achievementTitle = (code: string) =>
      catalog.value.find((item) => item.code === code)?.title || code

    const fail = (err: any, fallback: string) => {
      const data = err?.response?.data
      errorMsg.value = (typeof data === 'string' ? data : data?.error) || fallback
    }

    const loadAchievements = async () => {
      if (!props.user) return
      const [user, list] = await Promise.all([
        adminGetUserAchievements(props.user.id),
        // Каталог читается тем же правом, что и вкладка. Отказ по нему не должен
        // прятать выдачи: названия станут кодами, и это лучше пустого экрана.
        adminGetAchievements().catch(() => [] as AdminAchievement[]),
      ])
      grants.value = user.grants
      level.value = user.level
      catalog.value = list
      total.value = user.grants.length
    }

    const load = async (append = false) => {
      if (!props.user) return
      loading.value = true
      errorMsg.value = ''
      try {
        const offset = append ? shown.value : 0
        if (tab.value === 'achievements') {
          await loadAchievements()
        } else if (tab.value === 'penalties') {
          penalties.value = await getUserPenalties(props.user.id)
          total.value = penalties.value.points.length
        } else if (tab.value === 'transactions') {
          const res = await getUserTransactions(props.user.id, { limit: PAGE_SIZE, offset })
          transactions.value = append ? [...transactions.value, ...res.transactions] : res.transactions
          total.value = res.total
        } else {
          const res = await getUserOrders(props.user.id, { limit: PAGE_SIZE, offset })
          orders.value = append ? [...orders.value, ...res.orders] : res.orders
          total.value = res.total
        }
      } catch (err: any) {
        fail(err, 'Не удалось загрузить историю')
      } finally {
        loading.value = false
      }
    }

    // Действия вкладки ачивок. Каждое кончается перечитыванием выдач: результат
    // кнопки — это новая строка в таблице под ней, и показывать его иначе, чем
    // тем, что реально записалось, незачем.
    const runAction = async (job: () => Promise<string>) => {
      if (!props.user || busy.value) return
      busy.value = true
      errorMsg.value = ''
      actionMsg.value = ''
      try {
        actionMsg.value = await job()
        await loadAchievements()
      } catch (err: any) {
        fail(err, 'Действие не удалось')
      } finally {
        busy.value = false
      }
    }

    const recheck = () =>
      runAction(async () => {
        const result = await adminRecheckUserAchievements(props.user.id)
        if (!result.granted.length) {
          return `Прогнано заказов: ${result.orders_replayed}. Новых ачивок нет — условия не выполнены.`
        }
        const names = result.granted.map(achievementTitle).join(', ')
        return `Прогнано заказов: ${result.orders_replayed}. Выдано: ${names}.`
      })

    const recalcStats = () =>
      runAction(async () => {
        await adminRecalculateStats(props.user.id)
        return 'Агрегаты пересчитаны по журналу заказов. Теперь можно пересчитать условия.'
      })

    const grant = () =>
      runAction(async () => {
        const code = grantCode.value
        await adminGrantAchievement(props.user.id, code, grantReason.value)
        grantCode.value = ''
        grantReason.value = ''
        return `Ачивка «${achievementTitle(code)}» выдана вручную.`
      })

    const revoke = (row: UserGrant) =>
      runAction(async () => {
        await adminRevokeGrant(row.id, 'отозвана администратором')
        return `Выдача «${achievementTitle(row.code)}» отозвана, её баллы больше не считаются.`
      })

    const runPenaltyAction = async (job: () => Promise<string>) => {
      if (!props.user || busy.value) return
      busy.value = true
      errorMsg.value = ''
      actionMsg.value = ''
      try {
        actionMsg.value = await job()
        penalties.value = await getUserPenalties(props.user.id)
      } catch (err: any) {
        fail(err, 'Действие не удалось')
      } finally {
        busy.value = false
      }
    }

    // Вернуть в работу можно только заказ на проверке; после ответа лента
    // перечитывается, чтобы статус в строке был тем, что записал сервер.
    const returnToWork = async (order: UserOrder) => {
      if (!props.user || busy.value || !window.confirm(RETURN_TO_WORK_CONFIRM)) return
      busy.value = true
      errorMsg.value = ''
      actionMsg.value = ''
      try {
        await returnOrderToWork(order.id)
        actionMsg.value = 'Заказ возвращён в работу.'
        await load()
      } catch (err: any) {
        fail(err, 'Не удалось вернуть заказ в работу')
      } finally {
        busy.value = false
      }
    }

    const revokePoint = (point: PenaltyPoint) => {
      if (!window.confirm('Отменить штрафной балл? Период фото и блокировки пересчитаются сразу.')) return
      return runPenaltyAction(async () => {
        await revokePenaltyPoint(props.user.id, point.id)
        return 'Балл отменён, состояние пересчитано.'
      })
    }

    const resetFlag = () =>
      runPenaltyAction(async () => {
        await resetSilentBlockFlag(props.user.id)
        return 'Флаг прошлой тихой блокировки снят.'
      })

    const statusFor = (role: string) => penalties.value?.statuses.find((st) => st.role === role) || null
    const activeUntil = (value?: string) => !!value && new Date(value).getTime() > Date.now()

    const switchTo = (next: Tab) => {
      if (tab.value === next) return
      tab.value = next
      total.value = 0
      actionMsg.value = ''
      load()
    }

    const loadMore = () => load(true)

    // Окно переиспользуется между пользователями, поэтому при каждом открытии
    // списки сбрасываются: иначе на новом пользователе на мгновение видна чужая
    // история.
    watch(
      () => [props.modelValue, props.user?.id] as const,
      ([open]) => {
        if (!open) return
        tab.value = props.initialTab
        transactions.value = []
        orders.value = []
        grants.value = []
        penalties.value = null
        actionMsg.value = ''
        grantCode.value = ''
        grantReason.value = ''
        total.value = 0
        load()
      },
    )

    const formatDate = (value: string) =>
      new Date(value).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })

    const formatAmount = (value: number) => `${Number(value ?? 0).toFixed(2)} ₽`

    const formatSigned = (value: number, direction: number) => {
      const sign = direction > 0 ? '+' : direction < 0 ? '−' : ''
      return `${sign}${Number(value ?? 0).toFixed(2)} ₽`
    }

    const signClass = (direction: number) =>
      direction > 0 ? 'positive' : direction < 0 ? 'negative' : ''

    const typeLabel = (type: string) => TYPE_LABELS[type] || type
    const statusLabel = (status: string) => ORDER_STATUS_LABELS[status] || status

    // Один и тот же человек мог быть в заказе и заказчиком, и исполнителем —
    // лента общая, поэтому роль подписывается у каждой строки.
    const roleIn = (order: UserOrder) => {
      if (!props.user) return ''
      if (order.customer_id === props.user.id) return 'заказчик'
      if (order.executor_id === props.user.id) return 'исполнитель'
      return '—'
    }

    return {
      tab,
      transactions,
      orders,
      grants,
      level,
      weightSum,
      grantable,
      grantCode,
      grantReason,
      busy,
      actionMsg,
      canSeeAchievements,
      canRevoke,
      achievementTitle,
      recheck,
      recalcStats,
      grant,
      revoke,
      total,
      shown,
      loading,
      errorMsg,
      fullName,
      title,
      switchTo,
      penalties,
      canSeePenalties,
      canEditPenalties,
      canEditOrders,
      canReturnToWork,
      returnToWork,
      revokePoint,
      resetFlag,
      statusFor,
      activeUntil,
      loadMore,
      formatDate,
      formatAmount,
      formatSigned,
      signClass,
      typeLabel,
      statusLabel,
      roleIn,
    }
  },
})
</script>

<style scoped>
.history-sub {
  color: #6b7280;
  font-size: 13px;
  margin: 0 0 12px;
}

.tabs {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.tab {
  border: 1px solid #e5e7eb;
  background: #fff;
  border-radius: 10px;
  padding: 6px 14px;
  font-size: 13px;
  font-weight: 600;
  color: #475569;
  cursor: pointer;
}

.tab.active {
  background: #111827;
  border-color: #111827;
  color: #fff;
}

.alert {
  padding: 10px 12px;
  border-radius: 10px;
  font-size: 13px;
  margin-bottom: 10px;
}

.alert.error {
  background: #fef2f2;
  color: #b91c1c;
}

.alert.success {
  background: #ecfdf5;
  color: #047857;
}

/* --- Вкладка ачивок --- */

.achievements {
  max-height: 55vh;
  overflow: auto;
}

.level-line {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13px;
  color: #4b5563;
  margin-bottom: 12px;
}

.level-badge {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: #111827;
  color: #f59e0b;
  font-weight: 700;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.ach-actions,
.ach-grant {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 8px;
}

.btn-action {
  border: 1px solid #e5e7eb;
  background: #fff;
  border-radius: 10px;
  padding: 8px 12px;
  font-size: 13px;
  color: #374151;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.btn-action:disabled {
  opacity: 0.5;
  cursor: default;
}

.btn-action.primary {
  background: #111827;
  border-color: #111827;
  color: #fff;
}

.grant-select,
.grant-reason {
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  padding: 8px 10px;
  font-size: 13px;
  min-width: 0;
}

.grant-select {
  flex: 1 1 220px;
}

.grant-reason {
  flex: 1 1 180px;
}

.hint {
  font-size: 12px;
  color: #9ca3af;
  margin: 0 0 12px;
}

.btn-link {
  border: none;
  background: none;
  font-size: 13px;
  color: #4b5563;
  cursor: pointer;
  padding: 0;
}

.btn-link.danger {
  color: #b91c1c;
}

/* Отозванная выдача остаётся в таблице: карточка — это история, а не витрина. */
.history-table tr.revoked td {
  opacity: 0.55;
  text-decoration: line-through;
}

/* История длиннее экрана — прокручивается таблица, а не всё окно. */
.table-scroll {
  max-height: 55vh;
  overflow: auto;
}

.history-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.history-table th {
  text-align: left;
  color: #6b7280;
  font-weight: 500;
  padding: 8px 10px;
  border-bottom: 1px solid #eef0f4;
  position: sticky;
  top: 0;
  background: #fff;
}

.history-table td {
  padding: 9px 10px;
  border-bottom: 1px solid #f5f6f8;
  vertical-align: top;
}

.history-table .num {
  text-align: right;
  white-space: nowrap;
}

.nowrap {
  white-space: nowrap;
}

.positive {
  color: #047857;
  font-weight: 600;
}

.negative {
  color: #b91c1c;
  font-weight: 600;
}

.mono {
  font-family: ui-monospace, monospace;
}

.muted {
  color: #9ca3af;
  font-size: 12px;
}

.empty {
  color: #9ca3af;
  text-align: center;
  padding: 18px;
}

.history-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 12px;
  flex-wrap: wrap;
}

.foot-actions {
  display: flex;
  gap: 8px;
}

.btn-more,
.btn-close {
  border: 1px solid #e5e7eb;
  background: #fff;
  border-radius: 10px;
  padding: 7px 14px;
  font-size: 13px;
  cursor: pointer;
}

.btn-close {
  background: #111827;
  border-color: #111827;
  color: #fff;
}

.btn-more:disabled {
  opacity: 0.5;
  cursor: default;
}
.penalty-roles { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 10px; margin-bottom: 10px; }
.penalty-role { background: #f8fafc; border-radius: 12px; padding: 10px 12px; font-size: 13px; line-height: 1.6; }
.penalty-role-title { font-weight: 700; font-size: 14px; }
.penalty-flags { font-size: 13px; line-height: 1.6; margin-bottom: 10px; }
.danger-text { color: #b91c1c; }
</style>
