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
        <button type="button" class="location-btn" title="Моё местоположение" @click="recenterOnMe">
          <i class="ph-fill ph-navigation-arrow"></i>
        </button>

        <!-- Cluster Overlay: several orders sharing one pickup point -->
        <div v-if="selectedCluster && !selectedOrder" class="order-preview-card cluster-list-card">
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
              @click="openClusterOrder(o)"
            >
              <div class="ci-left">
                <div class="ci-icon" :class="{ hot: o.is_asap || o.is_urgent }">
                  <i class="ph-fill" :class="(o.is_asap || o.is_urgent) ? 'ph-lightning' : 'ph-package'"></i>
                </div>
                <div class="ci-text">
                  <div class="ci-price">{{ currencySymbol }}{{ Number(o.hold_amount).toFixed(0) }}</div>
                  <div class="ci-dist" :class="o.can_accept ? 'ok' : 'far'">
                    {{ o.can_accept ? 'Можно взять' : '> 2 км' }}
                  </div>
                </div>
              </div>
              <i class="ph-bold ph-caret-right ci-arrow"></i>
            </button>
          </div>
        </div>

        <!-- Selected Order Overlay Card -->
        <div v-if="selectedOrder" class="order-preview-card">
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
          <div class="op-header">
            <span class="op-id">#{{ selectedOrder?.id ? selectedOrder.id.slice(0, 8) : '---' }}</span>
            <span class="op-price">{{ currencySymbol }}{{ Number(selectedOrder.hold_amount).toFixed(2) }}</span>
          </div>
          <div class="op-address">{{ selectedOrder.address || 'Адрес не указан' }}</div>
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

export default defineComponent({
  name: 'ExecutorMapModal',
  props: {
    modelValue: { type: Boolean, required: true },
    currentLat: { type: Number, default: 55.7558 },
    currentLon: { type: Number, default: 37.6173 },
    currencySymbol: { type: String, default: '₽' },
  },
  emits: ['update:modelValue', 'order-accepted', 'location-changed'],
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

        // Initialize Leaflet map without default zoomControl (custom controls used)
        map = L.map(container, { zoomControl: false }).setView([serverLat.value, serverLon.value], 14)

        // Tile layer
        L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
          maxZoom: 19,
          attribution: '© OpenStreetMap',
        }).addTo(map)

        // 10km Outer Circle — faint overview hint
        zone50kmCircle = L.circle([serverLat.value, serverLon.value], {
          radius: 10000,
          color: '#5c60f5',
          weight: 1,
          dashArray: '4, 8',
          fillColor: '#5c60f5',
          fillOpacity: 0.02,
        }).addTo(map)

        // 0.5km (500m) Search Zone Circle from template
        zone2kmCircle = L.circle([serverLat.value, serverLon.value], {
          radius: 500,
          color: '#5c60f5',
          weight: 2,
          dashArray: '8, 6',
          fillColor: '#5c60f5',
          fillOpacity: 0.08,
        }).addTo(map)

        // User Draggable Dot Marker with Pulse
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

        userMarker.on('dragend', async (event: any) => {
          const position = event.target.getLatLng()
          await handleManualLocationChange(position.lat, position.lng)
        })

        map.on('click', async (event: L.LeafletMouseEvent) => {
          const { lat, lng } = event.latlng
          if (userMarker) {
            userMarker.setLatLng([lat, lng])
          }
          await handleManualLocationChange(lat, lng)
        })

        markersLayer = L.layerGroup().addTo(map)
        fetchMapOrders()

        const updateViewBounds = () => {
          if (!map) return
          map.invalidateSize()
          const viewRadius = 2500
          const viewBounds = L.circle([serverLat.value, serverLon.value], { radius: viewRadius }).getBounds()
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

    const renderMarkers = () => {
      if (!markersLayer || !map) return
      markersLayer.clearLayers()

      const groups = new Map<string, any[]>()
      mapOrders.value.forEach((order) => {
        const oLat = order.pickup_lat || 55.7558
        const oLon = order.pickup_lon || 37.6173
        const key = `${oLat.toFixed(5)},${oLon.toFixed(5)}`
        const list = groups.get(key)
        if (list) list.push(order)
        else groups.set(key, [order])
      })

      groups.forEach((orders) => {
        const oLat = orders[0].pickup_lat || 55.7558
        const oLon = orders[0].pickup_lon || 37.6173

        if (orders.length === 1) {
          const order = orders[0]
          const hot = !!(order.is_asap || order.is_urgent)
          const price = Number(order.hold_amount || 0).toFixed(0)

          const orderIcon = L.divIcon({
            className: 'tmpl-marker',
            html: `<div class="tmpl-pin ${hot ? 'hot' : ''} ${order.can_accept ? 'acceptable' : ''}">
                     <i class="ph-fill ${hot ? 'ph-lightning' : 'ph-package'} pin-icon"></i>
                     <span>${price} ${props.currencySymbol}</span>
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
            html: `<div class="cluster-pin ${anyAccept ? 'acceptable' : ''}">
                     <i class="ph-fill ph-stack pin-icon"></i>
                     <span>Заказы</span>
                     <div class="notification-badge">${orders.length}</div>
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

    const recenterOnMe = () => {
      if (map) map.setView([serverLat.value, serverLon.value], 15, { animate: true })
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

/* Selected Order Overlay Card */
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

/* Cluster list overlay */
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

/* User dot marker */
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

/* Single Order Pin */
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

/* Cluster Pin */
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
