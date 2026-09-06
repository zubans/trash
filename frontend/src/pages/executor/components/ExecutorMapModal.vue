<template>
  <div v-if="show" class="map-modal-overlay" @click.self="show = false">
    <div class="bottom-sheet">
      <!-- Шапка -->
      <div class="sheet-header">
        <div class="drag-handle"></div>
        <div class="header-content">
          <div class="header-text">
            <h2>{{ $t('executor.workAreaTitle', 'Район работы') }}</h2>
            <p>{{ $t('executor.workAreaSubtitle', 'Нажмите на карту за пределами круга, чтобы сместить центр зоны (0.5 км)') }}</p>
          </div>
          <button type="button" class="close-btn" :aria-label="$t('common.close', 'Закрыть')" @click="show = false">
            <i class="ph-bold ph-x"></i>
          </button>
        </div>
      </div>

      <!-- Карта -->
      <div class="map-area">
        <div id="executor-leaflet-map" class="leaflet-container-box"></div>

        <!-- Кнопки зума -->
        <div class="zoom-controls">
          <button type="button" class="zoom-btn" title="Увеличить" @click="zoomIn">
            <i class="ph-bold ph-plus"></i>
          </button>
          <button type="button" class="zoom-btn" title="Уменьшить" @click="zoomOut">
            <i class="ph-bold ph-minus"></i>
          </button>
        </div>

        <!-- Геолокация -->
        <button
          type="button"
          class="location-btn"
          :class="{ busy: locating }"
          :disabled="locating"
          title="Моё местоположение"
          @click="recenterOnMe"
        >
          <i :class="locating ? 'ph-bold ph-spinner spin' : 'ph-fill ph-navigation-arrow'"></i>
        </button>

        <!-- Подтверждение смены рабочего района -->
        <div v-if="pendingMove" class="order-preview-card move-confirm-card">
          <div class="mc-title">
            <i class="ph-fill ph-map-pin-line"></i> Сменить рабочий район?
          </div>
          <div class="mc-text">
            Метка переедет на {{ pendingDistanceKM.toFixed(1) }} км.
            Следующая смена района будет доступна только через 10 минут.
          </div>
          <div class="mc-actions">
            <button type="button" class="mc-btn cancel" @click="cancelMove">Отмена</button>
            <button type="button" class="mc-btn confirm" @click="confirmMove">Перенести</button>
          </div>
        </div>

        <!-- Оверлей кластера: несколько заказов в одной точке подачи -->
        <div v-if="selectedCluster && !selectedOrder && !pendingMove" class="order-preview-card cluster-list-card">
          <button type="button" class="btn-close-card" @click="selectedCluster = null">
            <i class="ph-bold ph-x"></i>
          </button>
          <div class="cluster-title">
            {{ selectedCluster.length }} заказов по этому адресу
          </div>
          <div class="cluster-address">{{ selectedCluster[0].address || 'Адрес не указан' }}</div>
          <div class="cluster-rows">
            <button
              v-for="o in selectedCluster"
              :key="o.id"
              type="button"
              class="cluster-item"
              :class="o.can_accept ? 'ok' : 'far'"
              @click="openClusterOrder(o)"
            >
              <span class="ci-dot"></span>
              <div class="ci-text">
                <div class="ci-title">{{ serviceTitle(o) }}</div>
                <div class="ci-meta">
                  <span class="ci-price">{{ Number(o.hold_amount).toFixed(0) }} {{ currencySymbol }}</span>
                  <span v-if="shortAddress(o)" class="ci-addr">· {{ shortAddress(o) }}</span>
                </div>
              </div>
              <i class="ph-bold ph-caret-right ci-arrow"></i>
            </button>
          </div>
        </div>

        <!-- Карточка-оверлей выбранного заказа -->
        <div v-if="selectedOrder && !pendingMove" class="order-preview-card">
          <button
            v-if="selectedCluster"
            type="button"
            class="btn-back-cluster"
            @click="selectedOrder = null"
          >
            <i class="ph-bold ph-arrow-left"></i> К списку
          </button>
          <button type="button" class="btn-close-card" @click="selectedOrder = null; selectedCluster = null">
            <i class="ph-bold ph-x"></i>
          </button>
          <div class="op-title">{{ serviceTitle(selectedOrder) }}</div>
          <div class="op-header">
            <span class="op-price">{{ Number(selectedOrder.hold_amount).toFixed(0) }} {{ currencySymbol }}</span>
            <span class="op-accept-tag" :class="selectedOrder.can_accept ? 'ok' : 'far'">
              <!-- Расстояние, а не зашитое число: подпись говорила «> 2 км»,
                   когда радиус взятия был 0.5 км, и исполнителю показывалась
                   граница, которой нет. -->
              {{ selectedOrder.can_accept ? 'Можно взять' : 'Далеко: ' + selectedOrder.distance_km.toFixed(1) + ' км' }}
            </span>
          </div>
          <div class="op-address">
            <i class="ph-fill ph-map-pin"></i>
            {{ shortAddress(selectedOrder) || selectedOrder.address || 'Адрес не указан' }}
          </div>
          <div class="op-distance">
            📏 Дистанция: <strong>{{ selectedOrder.distance_km.toFixed(1) }} км</strong>
          </div>

          <div class="op-actions mt-3">
            <button
              v-if="selectedOrder.can_accept"
              type="button"
              class="btn-accept-map"
              :disabled="accepting"
              @click="acceptMapOrder"
            >
              <span v-if="accepting" class="spinner-sm"></span>
              <template v-else>
                Взять заказ <i class="ph-bold ph-check"></i>
              </template>
            </button>
            <div v-else class="btn-disabled-hint">
              ⚠️ Заказ вне зоны взятия (> 2 км). Подойдите ближе или переставьте метку района.
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import api from '../../../services/api'
import { getCurrentCoordinates, geolocationMessage, GeolocationError } from '../../../services/geolocation'

export default defineComponent({
  name: 'ExecutorMapModal',
  props: {
    modelValue: { type: Boolean, required: true },
    currentLat: { type: Number, default: 55.7558 },
    currentLon: { type: Number, default: 37.6173 },
    currencySymbol: { type: String, default: '₽' },
  },
  emits: ['update:modelValue', 'order-accepted', 'location-changed', 'error'],
  setup(props, { emit }) {
    const { t } = useI18n()
    const show = computed({
      get: () => props.modelValue,
      set: (val) => emit('update:modelValue', val),
    })

    const selectedOrder = ref<any>(null)
    const selectedCluster = ref<any[] | null>(null)
    const accepting = ref(false)

    const serverLat = ref(props.currentLat)
    const serverLon = ref(props.currentLon)

    let moving = false
    let map: L.Map | null = null
    let markersLayer: L.LayerGroup | null = null
    let userMarker: L.Marker | null = null
    let zone2kmCircle: L.Circle | null = null
    let zone50kmCircle: L.Circle | null = null

    const mapOrders = ref<any[]>([])

    const fetchServerLocation = async (): Promise<boolean> => {
      try {
        const res = await api.get('/executor/location')
        if (res.data && res.data.has_location && res.data.lat != null && res.data.lon != null) {
          serverLat.value = res.data.lat
          serverLon.value = res.data.lon
          return true
        }
      } catch (err) {
        console.error('Failed to fetch executor location:', err)
      }
      return false
    }

    const fetchMapOrders = async () => {
      try {
        const res = await api.get('/executor/map-orders', {
          params: { lat: props.currentLat, lon: props.currentLon },
        })
        mapOrders.value = res.data || []
        renderMarkers()
      } catch (err) {
        console.error('Failed to fetch map orders:', err)
      }
    }

    const initMap = () => {
      nextTick(() => {
        const container = document.getElementById('executor-leaflet-map')
        if (!container) return
        if (map) {
          map.remove()
          map = null
        }

        // Инициализируем карту Leaflet без стандартного zoomControl (используются свои кнопки)
        map = L.map(container, { zoomControl: false }).setView([serverLat.value, serverLon.value], 14)

        // Слой тайлов
        L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
          maxZoom: 19,
          attribution: '© OpenStreetMap',
        }).addTo(map)

        // Внешний круг 10 км — бледная обзорная подсказка
        zone50kmCircle = L.circle([serverLat.value, serverLon.value], {
          radius: 10000,
          color: '#5c60f5',
          weight: 1,
          dashArray: '4, 8',
          fillColor: '#5c60f5',
          fillOpacity: 0.02,
        }).addTo(map)

        // Круг зоны поиска 0,5 км (500 м) из шаблона
        zone2kmCircle = L.circle([serverLat.value, serverLon.value], {
          radius: 500,
          color: '#5c60f5',
          weight: 2,
          dashArray: '8, 6',
          fillColor: '#5c60f5',
          fillOpacity: 0.08,
        }).addTo(map)

        // Перетаскиваемая точка-маркер пользователя с пульсацией
        const userIcon = L.divIcon({
          className: 'zone-dot-wrapper',
          html: `<div class="zone-dot-marker">
                   <div class="zone-dot-pulse"></div>
                   <div class="zone-dot"></div>
                 </div>`,
          iconSize: [24, 24],
          iconAnchor: [12, 12],
        })

        userMarker = L.marker([serverLat.value, serverLon.value], {
          draggable: true,
          icon: userIcon,
        }).addTo(map)

        // Оба жеста только предлагают перемещение. Смена рабочей зоны запускает
        // десятиминутную паузу, поэтому случайный тап или перетаскивание раньше
        // стоили исполнителю возможности переместиться на следующие десять минут —
        // перемещение фиксируется, только когда он его подтвердит.
        userMarker.on('dragend', (event: any) => {
          const position = event.target.getLatLng()
          proposeMove(position.lat, position.lng)
        })

        map.on('click', (event: L.LeafletMouseEvent) => {
          const { lat, lng } = event.latlng
          if (userMarker) {
            userMarker.setLatLng([lat, lng])
          }
          proposeMove(lat, lng)
        })

        markersLayer = L.layerGroup().addTo(map)
        fetchMapOrders()

        const updateViewBounds = () => {
          if (!map) return
          map.invalidateSize()
          const viewRadius = 2500
          const viewBounds = L.latLng(serverLat.value, serverLon.value).toBounds(viewRadius * 2)
          map.fitBounds(viewBounds, { animate: false })
        }

        updateViewBounds()
        setTimeout(updateViewBounds, 150)
        setTimeout(updateViewBounds, 350)
      })
    }

    const anchorTo = (lat: number, lon: number) => {
      serverLat.value = lat
      serverLon.value = lon
      if (userMarker) userMarker.setLatLng([lat, lon])
      if (zone2kmCircle) zone2kmCircle.setLatLng([lat, lon])
      if (zone50kmCircle) zone50kmCircle.setLatLng([lat, lon])
    }

    // Расстояние между двумя точками в километрах. Используется только чтобы
    // сказать исполнителю, насколько сдвинется метка, поэтому сферического
    // приближения более чем достаточно.
    const haversineKM = (lat1: number, lon1: number, lat2: number, lon2: number): number => {
      const toRad = (deg: number) => (deg * Math.PI) / 180
      const earthRadiusKM = 6371
      const dLat = toRad(lat2 - lat1)
      const dLon = toRad(lon2 - lon1)
      const a =
        Math.sin(dLat / 2) * Math.sin(dLat / 2) +
        Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) * Math.sin(dLon / 2)
      return earthRadiusKM * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a))
    }

    // Перемещение, которое исполнитель показал жестом, но ещё не подтвердил. Метка
    // уже стоит на нём, поэтому отмена обязана вернуть метку обратно.
    const pendingMove = ref<{ lat: number; lon: number } | null>(null)

    // Насколько предлагаемая точка отстоит от якоря, который сейчас держит сервер,
    // чтобы подтверждение сообщило, что на самом деле изменится.
    const pendingDistanceKM = computed(() => {
      if (!pendingMove.value) return 0
      return haversineKM(serverLat.value, serverLon.value, pendingMove.value.lat, pendingMove.value.lon)
    })

    const proposeMove = (lat: number, lon: number) => {
      if (moving) return
      pendingMove.value = { lat, lon }
    }

    const cancelMove = () => {
      pendingMove.value = null
      // Возвращаем метку в позицию, которую сервер всё ещё считает текущей.
      anchorTo(serverLat.value, serverLon.value)
    }

    const confirmMove = async () => {
      const target = pendingMove.value
      if (!target) return
      pendingMove.value = null
      await handleManualLocationChange(target.lat, target.lon)
    }

    const handleManualLocationChange = async (lat: number, lon: number) => {
      if (moving) return
      moving = true
      try {
        const res = await api.post('/executor/set-location', {
          lat,
          lon,
          is_manual: true,
        })
        if (res.data && res.data.success) {
          anchorTo(res.data.lat ?? lat, res.data.lon ?? lon)
          emit('location-changed', { lat: serverLat.value, lon: serverLon.value })
          fetchMapOrders()
        } else if (res.data && !res.data.success) {
          alert(res.data.message || 'Ручное перемещение отклонено')
          anchorTo(res.data.lat ?? serverLat.value, res.data.lon ?? serverLon.value)
        }
      } catch (err: any) {
        alert(err.response?.data?.message || err.response?.data || 'Ошибка изменения метки (10 мин кулдаун)')
        anchorTo(serverLat.value, serverLon.value)
      } finally {
        moving = false
      }
    }

    // Защищаем HTML в divIcon от инъекции: названия и адреса приходят из данных
    // пользователя и каталога и попадают в innerHTML.
    const escapeHtml = (s: string): string =>
      String(s).replace(/[&<>"']/g, (c) =>
        ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c] as string),
      )

    // «Категория · Услуга» из полей, которые прикрепляет эндпоинт карты.
    const serviceTitle = (o: any): string => {
      const variant =
        o?.service_variant?.name?.ru ||
        o?.service_variant?.name?.en ||
        o?.service_variant?.code ||
        ''
      const category = o?.category_name || ''
      if (category && variant) return `${category} · ${variant}`
      return variant || category || 'Услуга'
    }

    // Адрес без города: оставляем улицу, дом, квартиру.
    const streetLike =
      /^(ул\.?|улица|пер\.?|переул|пр-?кт|пр\.?|проспект|просп|б-?р|бул|наб\.?|ш\.?|шоссе|пл\.?|площад|проезд|туп\.?|аллея|линия|мкр|микрорайон|кв-?л|квартал)/i
    const shortAddress = (o: any): string => {
      const raw = (o?.address || '').trim()
      if (!raw) return ''
      const parts = raw.split(',').map((p: string) => p.trim()).filter(Boolean)
      const idx = parts.findIndex((p: string) => streetLike.test(p))
      const kept = idx >= 0 ? parts.slice(idx) : parts.slice(1)
      return (kept.length ? kept : parts).join(', ')
    }

    // Группируем заказы по зданию (улица + дом, квартира отброшена), чтобы
    // несколько заказов в одном доме — даже в разных квартирах — сливались в одну
    // метку, а не громоздились друг на друге так, что виден только верхний. При
    // отсутствии адреса откатываемся к координатам.
    const buildingKey = (o: any): string => {
      const addr = (o?.address || '').toLowerCase().trim()
      const building = addr
        .replace(/,?\s*(кв\.?|квартира|офис|оф\.?|помещение|пом\.?)\s*[^,]*/gi, '')
        .trim()
      if (building) return 'a:' + building
      const lat = (o.pickup_lat || 0).toFixed(5)
      const lon = (o.pickup_lon || 0).toFixed(5)
      return `c:${lat},${lon}`
    }

    const renderMarkers = () => {
      if (!markersLayer || !map) return
      markersLayer.clearLayers()

      const groups = new Map<string, any[]>()
      mapOrders.value.forEach((order) => {
        const key = buildingKey(order)
        const list = groups.get(key)
        if (list) list.push(order)
        else groups.set(key, [order])
      })

      groups.forEach((orders) => {
        const oLat = orders[0].pickup_lat || 55.7558
        const oLon = orders[0].pickup_lon || 37.6173

        if (orders.length === 1) {
          const order = orders[0]
          const price = Number(order.hold_amount || 0).toFixed(0)

          // То, на что метка обязана ответить с первого взгляда, — «что это за работа?»,
          // поэтому она несёт категорию и услугу. Адрес был единственным, что карта и так
          // показывает — тем, где стоит метка, — и он выдавливал название услуги из
          // такой маленькой подписи.
          const orderIcon = L.divIcon({
            className: 'tmpl-marker',
            html: `<div class="tmpl-pin ${order.can_accept ? 'green' : 'yellow'}">
                     <div class="pin-title">${escapeHtml(serviceTitle(order))}</div>
                     <div class="pin-sub"><b>${price} ${props.currencySymbol}</b></div>
                   </div>`,
            iconSize: [0, 0],
            iconAnchor: [0, 0],
          })

          const marker = L.marker([oLat, oLon], { icon: orderIcon })
          marker.on('click', () => {
            selectedCluster.value = null
            selectedOrder.value = order
          })
          markersLayer?.addLayer(marker)
        } else {
          const anyAccept = orders.some((o) => o.can_accept)
          const clusterIcon = L.divIcon({
            className: 'tmpl-marker',
            html: `<div class="tmpl-pin cluster ${anyAccept ? 'green' : 'yellow'}">
                     <i class="ph-fill ph-stack pin-icon"></i>
                     <span>${orders.length} заказов</span>
                   </div>`,
            iconSize: [0, 0],
            iconAnchor: [0, 0],
          })

          const marker = L.marker([oLat, oLon], { icon: clusterIcon })
          marker.on('click', () => {
            selectedOrder.value = null
            selectedCluster.value = orders
          })
          markersLayer?.addLayer(marker)
        }
      })
    }

    const openClusterOrder = (order: any) => {
      selectedOrder.value = order
    }

    const zoomIn = () => {
      if (map) map.zoomIn()
    }

    const zoomOut = () => {
      if (map) map.zoomOut()
    }

    const locating = ref(false)

    // «Моё местоположение» — единственное, что возвращает рабочую зону устройству.
    // Пока у исполнителя район приколот вручную, периодические отчёты продолжают
    // записывать, где телефон, но якорь не трогают, поэтому именно эта кнопка
    // снова их синхронизирует — и центрирует карту на результате, а не на позиции,
    // которую сервер больше не держит.
    const recenterOnMe = async () => {
      if (locating.value) return
      locating.value = true
      try {
        const device = await getCurrentCoordinates()
        const res = await api.post('/executor/follow-device', { lat: device.lat, lon: device.lon })
        const lat = res.data?.lat ?? device.lat
        const lon = res.data?.lon ?? device.lon
        anchorTo(lat, lon)
        emit('location-changed', { lat, lon })
        if (map) map.setView([lat, lon], 15, { animate: true })
        fetchMapOrders()
      } catch (err: any) {
        // Позиция, которую не удалось прочитать, и упавший запрос — разные проблемы, а
        // исполнитель может что-то сделать только с первой.
        if (err instanceof GeolocationError) {
          emit('error', geolocationMessage(err))
        } else {
          emit('error', err?.response?.data?.message || err?.response?.data || 'Не удалось обновить местоположение')
        }
      } finally {
        locating.value = false
      }
    }

    const acceptMapOrder = async () => {
      if (!selectedOrder.value || accepting.value) return
      accepting.value = true
      try {
        await api.post(`/executor/orders/${selectedOrder.value.id}/accept`)
        emit('order-accepted', selectedOrder.value.id)
        selectedOrder.value = null
        selectedCluster.value = null
        show.value = false
      } catch (err: any) {
        const rawErr = err.response?.data
        const rawText = typeof rawErr === 'string' ? rawErr : (rawErr?.error || '')
        if (rawText.includes('executor has no active shift') || rawText.includes('no active shift') || rawText.includes('нет активной смены')) {
          alert(t('executor.noActiveShift'))
        } else if (rawText.includes('penalized') || rawText.includes('оштрафована')) {
          alert(t('executor.shiftPenalized'))
        } else {
          alert(rawText || t('executor.errorAcceptOrder', 'Ошибка принятия заказа'))
        }
      } finally {
        accepting.value = false
      }
    }

    watch(show, async (val) => {
      if (!val) return
      const hasServer = await fetchServerLocation()
      if (!hasServer) {
        serverLat.value = props.currentLat
        serverLon.value = props.currentLon
      }
      initMap()
    }, { immediate: true })

    return {
      show,
      selectedOrder,
      selectedCluster,
      accepting,
      openClusterOrder,
      acceptMapOrder,
      recenterOnMe,
      zoomIn,
      zoomOut,
      // Карточки заказов их рисуют; без них шаблон падает, и Vue размонтирует
      // всю модалку в момент нажатия на метку.
      serviceTitle,
      shortAddress,
      locating,
      pendingMove,
      pendingDistanceKM,
      confirmMove,
      cancelMove,
    }
  },
})
</script>

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=Nunito:wght@500;600;700;800;900&display=swap');

:root {
  --brand-primary: #5c60f5;
  --brand-primary-rgb: 92, 96, 245;
  --danger: #ef4444;
  --text-main: #0f172a;
  --text-muted: #64748b;
  --surface: #ffffff;
  --map-bg: #e2e8f0;
}

.map-modal-overlay {
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(15, 23, 42, 0.65);
  backdrop-filter: blur(8px);
  z-index: 9999;
  display: flex;
  justify-content: center;
  align-items: flex-end;
  animation: fadeIn 0.2s ease-out;
  font-family: 'Nunito', sans-serif;
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

/* Модальное окно (Bottom Sheet) */
.bottom-sheet {
  width: 100%;
  max-width: 480px;
  height: 85vh;
  background: #e2e8f0;
  position: relative;
  overflow: hidden;
  border-top-left-radius: 32px;
  border-top-right-radius: 32px;
  box-shadow: 0 -10px 40px rgba(0, 0, 0, 0.25);
  display: flex;
  flex-direction: column;
  animation: slideUp 0.3s cubic-bezier(0.16, 1, 0.3, 1);
  font-family: 'Nunito', sans-serif;
}

@keyframes slideUp {
  from { transform: translateY(100%); }
  to { transform: translateY(0); }
}

/* --- ЧИСТАЯ ШАПКА --- */
.sheet-header {
  background: #ffffff;
  padding: 24px 20px 20px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.05);
  position: relative;
  z-index: 100;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.03);
  border-bottom-left-radius: 24px;
  border-bottom-right-radius: 24px;
}

/* Индикатор свайпа (Drag handle) */
.drag-handle {
  width: 40px;
  height: 4px;
  background: #e2e8f0;
  border-radius: 4px;
  position: absolute;
  top: 8px;
  left: 50%;
  transform: translateX(-50%);
}

.header-content {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.header-text h2 {
  font-size: 20px;
  font-weight: 800;
  color: #0f172a;
  margin: 0 0 4px 0;
  letter-spacing: -0.5px;
  font-family: 'Nunito', sans-serif;
}

.header-text p {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
  line-height: 1.4;
  margin: 0;
  font-family: 'Nunito', sans-serif;
}

.close-btn {
  background: #f1f5f9;
  width: 32px;
  height: 32px;
  flex-shrink: 0;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  color: #64748b;
  border: none;
  cursor: pointer;
  transition: 0.2s;
}
.close-btn:active {
  background: #e2e8f0;
}

/* --- КАРТА --- */
.map-area {
  flex: 1;
  position: relative;
  overflow: hidden;
  width: 100%;
  height: 100%;
}

.leaflet-container-box {
  width: 100%;
  height: 100%;
}

/* Кнопки зума */
.zoom-controls {
  position: absolute;
  top: 20px;
  left: 20px;
  background: #ffffff;
  border-radius: 12px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08);
  overflow: hidden;
  display: flex;
  flex-direction: column;
  z-index: 500;
}

.zoom-btn {
  background: transparent;
  border: none;
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  font-weight: 800;
  color: #0f172a;
  cursor: pointer;
  transition: background 0.15s;
}
.zoom-btn:first-child {
  border-bottom: 1px solid #f1f5f9;
}
.zoom-btn:active {
  background: #f8fafc;
}

/* Кнопка геолокации */
.location-btn {
  position: absolute;
  bottom: 30px;
  right: 20px;
  background: #ffffff;
  border: none;
  border-radius: 50%;
  width: 50px;
  height: 50px;
  font-size: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
  color: #5c60f5;
  z-index: 500;
  cursor: pointer;
  transition: transform 0.15s ease;
}
.location-btn:active {
  transform: scale(0.92);
}

/* Карточка-оверлей выбранного заказа */
.order-preview-card {
  position: absolute;
  bottom: 20px;
  left: 16px;
  right: 16px;
  background: rgba(255, 255, 255, 0.96);
  backdrop-filter: blur(16px);
  border: 1px solid rgba(255, 255, 255, 0.9);
  border-radius: 24px;
  padding: 20px;
  box-shadow: 0 20px 40px -10px rgba(0, 0, 0, 0.2);
  z-index: 600;
  animation: cardSlideUp 0.25s ease-out;
  font-family: 'Nunito', sans-serif;
}

@keyframes cardSlideUp {
  from { opacity: 0; transform: translateY(20px); }
  to { opacity: 1; transform: translateY(0); }
}

.btn-close-card {
  position: absolute;
  top: 14px;
  right: 14px;
  background: #f1f5f9;
  border: none;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #64748b;
  font-size: 14px;
  cursor: pointer;
}

.op-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}
.op-id {
  font-size: 14px;
  font-weight: 800;
  color: #5c60f5;
  font-family: monospace;
}
.op-price {
  font-size: 20px;
  font-weight: 900;
  color: #0f172a;
}
.op-address {
  font-size: 14px;
  font-weight: 600;
  color: #475569;
  margin-bottom: 6px;
}
.op-distance {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
}

.btn-accept-map {
  width: 100%;
  padding: 14px;
  border-radius: 16px;
  border: none;
  background: #10b981;
  color: #ffffff;
  font-weight: 800;
  font-size: 16px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  box-shadow: 0 4px 14px rgba(16, 185, 129, 0.3);
  transition: all 0.2s ease;
  font-family: 'Nunito', sans-serif;
}
.btn-accept-map:hover {
  background: #059669;
}
.btn-accept-map:active {
  transform: scale(0.98);
}

.btn-disabled-hint {
  background: #fffbebfb;
  border: 1px solid #fde68a;
  color: #b45309;
  padding: 10px 14px;
  border-radius: 14px;
  font-size: 13px;
  font-weight: 600;
  text-align: center;
}

/* Кнопка «моё местоположение» во время определения координат */
.location-btn.busy {
  opacity: 0.7;
  cursor: progress;
}
.location-btn .spin {
  animation: locSpin 0.9s linear infinite;
}
@keyframes locSpin {
  to { transform: rotate(360deg); }
}

/* Подтверждение смены района */
.move-confirm-card {
  padding-top: 20px;
}
.mc-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 17px;
  font-weight: 800;
  color: #0f172a;
}
.mc-title i {
  color: var(--brand-primary, #5c60f5);
  font-size: 20px;
}
.mc-text {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
  margin: 8px 0 16px;
  line-height: 1.4;
}
.mc-actions {
  display: flex;
  gap: 10px;
}
.mc-btn {
  flex: 1;
  border: none;
  border-radius: 14px;
  padding: 13px 16px;
  font-family: inherit;
  font-size: 15px;
  font-weight: 800;
  cursor: pointer;
  transition: filter 0.15s ease;
}
.mc-btn.cancel {
  background: #f1f5f9;
  color: #475569;
}
.mc-btn.confirm {
  background: var(--brand-primary, #5c60f5);
  color: #ffffff;
  box-shadow: 0 6px 16px rgba(92, 96, 245, 0.3);
}
.mc-btn:active {
  filter: brightness(0.95);
}

/* Оверлей списка кластера */
.cluster-list-card {
  padding-top: 22px;
}
.cluster-title {
  font-size: 17px;
  font-weight: 800;
  color: #0f172a;
  padding-right: 28px;
}
.cluster-address {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
  margin: 2px 0 12px;
}
.cluster-rows {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 40vh;
  overflow-y: auto;
}
.cluster-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  width: 100%;
  text-align: left;
  cursor: pointer;
  background: #f8fafc;
  border: none;
  border-radius: 16px;
  padding: 10px 14px;
  transition: background 0.15s ease;
  font-family: 'Nunito', sans-serif;
}
.cluster-item:hover {
  background: #eef2ff;
}
.ci-left {
  display: flex;
  align-items: center;
  gap: 12px;
}
.ci-icon {
  width: 36px;
  height: 36px;
  border-radius: 12px;
  flex-shrink: 0;
  background: #ffffff;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #5c60f5;
  font-size: 18px;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.06);
}
.ci-icon.hot {
  color: #f59e0b;
}
.ci-text {
  display: flex;
  flex-direction: column;
}
.ci-price {
  font-weight: 900;
  font-size: 16px;
  color: #0f172a;
}
.ci-dist {
  font-size: 12px;
  font-weight: 700;
}
.ci-dist.ok {
  color: #15803d;
}
.ci-dist.far {
  color: #b45309;
}
.ci-arrow {
  color: #cbd5e1;
  font-size: 16px;
}

.btn-back-cluster {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: transparent;
  border: none;
  color: #5c60f5;
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  padding: 0;
  margin-bottom: 10px;
  font-family: 'Nunito', sans-serif;
}
</style>

<style>
.leaflet-control-attribution {
  display: none !important;
}

/* Точка-маркер пользователя */
.zone-dot-marker {
  position: relative;
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.zone-dot {
  position: relative;
  width: 24px;
  height: 24px;
  background: #5c60f5;
  border: 4px solid #ffffff;
  border-radius: 50%;
  box-shadow: 0 4px 12px rgba(92, 96, 245, 0.4);
  z-index: 2;
}

.zone-dot-pulse {
  position: absolute;
  top: -8px;
  left: -8px;
  right: -8px;
  bottom: -8px;
  background: rgba(92, 96, 245, 0.25);
  border-radius: 50%;
  animation: zone-pulse 2s infinite;
  z-index: 1;
}

@keyframes zone-pulse {
  0% { transform: scale(0.6); opacity: 1; }
  100% { transform: scale(2.2); opacity: 0; }
}

/* Метка одиночного заказа */
.tmpl-pin {
  position: absolute;
  left: 0;
  top: 0;
  transform: translate(-50%, -100%);
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: #ffffff;
  color: #0f172a;
  padding: 6px 12px;
  border-radius: 20px;
  font-family: 'Nunito', sans-serif;
  font-weight: 800;
  font-size: 14px;
  white-space: nowrap;
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.12);
  cursor: pointer;
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}
.tmpl-pin::after {
  content: '';
  position: absolute;
  bottom: -5px;
  left: 50%;
  transform: translateX(-50%);
  border-width: 6px 6px 0;
  border-style: solid;
  border-color: #ffffff transparent transparent transparent;
}
.tmpl-pin:hover {
  transform: translate(-50%, -100%) scale(1.05);
  box-shadow: 0 10px 24px rgba(0, 0, 0, 0.2);
}
.tmpl-pin-icon {
  font-size: 16px;
  color: #5c60f5;
}
.tmpl-pin.hot .tmpl-pin-icon {
  color: #f59e0b;
}
.tmpl-pin.acceptable .tmpl-pin-icon {
  color: #10b981;
}

/* Метка кластера */
.cluster-pin {
  position: absolute;
  left: 0;
  top: 0;
  transform: translate(-50%, -100%);
  background: #ffffff;
  padding: 6px 12px;
  border-radius: 20px;
  font-family: 'Nunito', sans-serif;
  font-weight: 800;
  font-size: 14px;
  color: #0f172a;
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.12);
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  white-space: nowrap;
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}
.cluster-pin::after {
  content: '';
  position: absolute;
  bottom: -5px;
  left: 50%;
  transform: translateX(-50%);
  border-width: 6px 6px 0;
  border-style: solid;
  border-color: #ffffff transparent transparent transparent;
}
.cluster-pin:hover {
  transform: translate(-50%, -100%) scale(1.05);
  box-shadow: 0 10px 24px rgba(0, 0, 0, 0.2);
}
.cluster-pin.acceptable .pin-icon {
  color: #10b981;
}
.pin-icon {
  color: #5c60f5;
  font-size: 16px;
}
.notification-badge {
  position: absolute;
  top: -6px;
  right: -6px;
  background: #ef4444;
  color: #ffffff;
  font-size: 11px;
  font-weight: 900;
  width: 18px;
  height: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  border: 2px solid #ffffff;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.2);
}
</style>
