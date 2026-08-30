<template>
  <div v-if="show" class="map-modal-overlay" @click.self="show = false">
    <div class="map-modal-card">
      <div class="map-modal-header">
        <div>
          <h3 class="map-modal-title">Редактирование местоположения и карта заказов</h3>
          <p class="map-modal-subtitle">Кликните на карту за пределами круга 0.5 км, чтобы сменить рабочий район</p>
        </div>
        <button type="button" class="btn-close" aria-label="Закрыть" @click="show = false">
          <i class="ph ph-x"></i>
        </button>
      </div>

      <div class="map-modal-body">
        <div id="executor-leaflet-map" class="leaflet-container-box"></div>

        <!-- Cluster Overlay: several orders sharing one pickup point -->
        <div v-if="selectedCluster && !selectedOrder" class="order-preview-card cluster-list-card">
          <button type="button" class="btn-close-card" @click="selectedCluster = null">
            <i class="ph ph-x"></i>
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
              class="cluster-row"
              @click="openClusterOrder(o)"
            >
              <span class="cr-id">#{{ o.id ? o.id.slice(0, 8) : '---' }}</span>
              <span class="cr-price">{{ currencySymbol }}{{ Number(o.hold_amount).toFixed(2) }}</span>
              <span class="cr-badge" :class="o.can_accept ? 'ok' : 'far'">
                {{ o.can_accept ? 'Можно взять' : '> 2 км' }}
              </span>
              <i class="ph-bold ph-caret-right cr-arrow"></i>
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
            <i class="ph ph-x"></i>
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
import {defineComponent, ref, computed, watch, nextTick} from 'vue'
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
    const show = computed({
      get: () => props.modelValue,
      set: (val) => emit('update:modelValue', val),
    })

    const selectedOrder = ref<any>(null)
    // When several orders share one pickup point they are grouped behind a single
    // cluster marker; clicking it opens this list instead of a single card.
    const selectedCluster = ref<any[] | null>(null)
    const accepting = ref(false)
    // Authoritative position last confirmed by the server. The marker and both
    // zone circles are always anchored here — never directly to the props, which
    // can be stale — so an on-screen drag is measured against the same origin the
    // backend uses when it decides "within circle" vs "district change".
    const serverLat = ref(props.currentLat)
    const serverLon = ref(props.currentLon)
    // Guards against overlapping set-location requests. Without it a single
    // gesture can fire both `dragend` and a trailing map `click`; the second
    // request lands inside the 0.5 km circle of the just-moved point and comes
    // back rejected as a within-circle move, even though the real move was far.
    let moving = false
    let map: L.Map | null = null
    let markersLayer: L.LayerGroup | null = null
    let userMarker: L.Marker | null = null
    let zone2kmCircle: L.Circle | null = null
    let zone50kmCircle: L.Circle | null = null

    const mapOrders = ref<any[]>([])

    // Pull the authoritative stored position from the backend. This is what the
    // marker and both zone circles must anchor to, because the server decides
    // "within circle" vs "district change" against exactly this point. Props
    // (the device's own GPS fix) are only a fallback for an executor who has no
    // stored location yet.
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

        // Initialize Leaflet map with initial center and default zoom 14 (~2.5km radius view)
        map = L.map(container).setView([serverLat.value, serverLon.value], 14)

        // Tile layer attached immediately
        L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
          maxZoom: 19,
          attribution: '© OpenStreetMap',
        }).addTo(map)

        // 10km Outer Circle
        zone50kmCircle = L.circle([serverLat.value, serverLon.value], {
          radius: 10000,
          color: '#6366f1',
          weight: 1,
          dashArray: '6, 6',
          fillColor: '#6366f1',
          fillOpacity: 0.03,
        }).addTo(map)

        // 0.5km (500m) Accept Circle
        zone2kmCircle = L.circle([serverLat.value, serverLon.value], {
          radius: 500,
          color: '#10b981',
          weight: 2,
          fillColor: '#10b981',
          fillOpacity: 0.12,
        }).addTo(map)

        // User Draggable Marker
        const userIcon = L.divIcon({
          className: 'user-marker-pin',
          html: `<div class="user-pin-pulse"><i class="ph-bold ph-navigation-arrow"></i></div>`,
          iconSize: [32, 32],
          iconAnchor: [16, 16],
        })

        userMarker = L.marker([serverLat.value, serverLon.value], {
          draggable: true,
          icon: userIcon,
        }).addTo(map)

        userMarker.on('dragend', async (event: any) => {
          const position = event.target.getLatLng()
          await handleManualLocationChange(position.lat, position.lng)
        })

        // Move marker directly when clicking anywhere on the map
        map.on('click', async (event: L.LeafletMouseEvent) => {
          const { lat, lng } = event.latlng
          if (userMarker) {
            userMarker.setLatLng([lat, lng])
          }
          await handleManualLocationChange(lat, lng)
        })

        markersLayer = L.layerGroup().addTo(map)
        fetchMapOrders()

        // Scale view bounds to 5 * pickup zone diameter (500m radius -> 1000m diameter -> 5000m view diameter = 2500m view radius)
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

    // Snap the marker and both zone circles to a single point, and remember it
    // as the authoritative server position. Every outcome routes through here so
    // the map never drifts away from what the backend has stored.
    const anchorTo = (lat: number, lon: number) => {
      serverLat.value = lat
      serverLon.value = lon
      if (userMarker) userMarker.setLatLng([lat, lon])
      if (zone2kmCircle) zone2kmCircle.setLatLng([lat, lon])
      if (zone50kmCircle) zone50kmCircle.setLatLng([lat, lon])
    }

    const handleManualLocationChange = async (lat: number, lon: number) => {
      // Drop the gesture if a request is already in flight: a drag can emit both
      // `dragend` and a trailing map `click`, and letting the second one through
      // is exactly what produced the spurious "within circle" rejection.
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
          // The server echoes the position it kept; realign everything to it.
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

      // Group orders that resolve to (almost) the same pickup point — several
      // orders in one building or flat geocode to identical coordinates and would
      // otherwise stack directly on top of one another. Rounding to 5 decimals is
      // ~1 m, tight enough that only genuinely co-located orders merge.
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
          const canAccept = order.can_accept

          const orderIcon = L.divIcon({
            className: 'order-map-pin',
            html: `<div class="order-pin-bubble ${canAccept ? 'green' : 'orange'}">
                    ${canAccept ? '⚡' : '📍'} ${order.hold_amount}₽
                   </div>`,
            iconSize: [80, 30],
            iconAnchor: [40, 15],
          })

          const marker = L.marker([oLat, oLon], { icon: orderIcon })
          marker.on('click', () => {
            selectedCluster.value = null
            selectedOrder.value = order
          })
          markersLayer?.addLayer(marker)
        } else {
          // A distinct marker (purple, order count) signals several stacked
          // orders; clicking it reveals the mini list rather than one card.
          const anyAccept = orders.some((o) => o.can_accept)
          const clusterIcon = L.divIcon({
            className: 'order-map-pin',
            html: `<div class="order-cluster-bubble ${anyAccept ? 'has-accept' : ''}">
                    <span class="cluster-count">${orders.length}</span> заказов
                   </div>`,
            iconSize: [96, 32],
            iconAnchor: [48, 16],
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

    // Open a single order from the cluster list. The cluster stays set so the
    // preview card can offer a "back to list" affordance.
    const openClusterOrder = (order: any) => {
      selectedOrder.value = order
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
        alert(err.response?.data || 'Ошибка принятия заказа')
      } finally {
        accepting.value = false
      }
    }

    watch(show, async (val) => {
      if (!val) return
      // Resolve the center from the server every time the modal opens, so it
      // always reflects the last confirmed position instead of whatever props
      // happened to hold when this component was first mounted. Fall back to the
      // props only when the backend has no stored location yet.
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
      openClusterOrder,
      accepting,
      acceptMapOrder,
    }
  },
})
</script>

<style scoped>
.map-modal-overlay {
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(15, 23, 42, 0.75);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  display: flex; align-items: center; justify-content: center;
  padding: 20px; z-index: 9999;
}

.map-modal-card {
  background: #ffffff;
  border-radius: 28px;
  width: 100%; max-width: 900px; height: 85vh;
  display: flex; flex-direction: column;
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
  overflow: hidden; position: relative;
}

.map-modal-header {
  padding: 20px 24px; display: flex; justify-content: space-between; align-items: center;
  border-bottom: 1px solid #f1f5f9;
}
.map-modal-title { font-size: 20px; font-weight: 700; color: #0f172a; margin: 0; }
.map-modal-subtitle { font-size: 13px; color: #64748b; margin: 2px 0 0 0; }

.btn-close {
  width: 36px; height: 36px; border-radius: 50%; border: none;
  background: #f1f5f9; color: #64748b; font-size: 18px; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
}

.map-modal-body {
  flex: 1; position: relative; width: 100%; height: 100%;
}

.leaflet-container-box {
  width: 100%; height: 100%;
}

/* Selected Order Card Overlay */
.order-preview-card {
  position: absolute; bottom: 24px; left: 24px; right: 24px;
  background: rgba(255, 255, 255, 0.95); backdrop-filter: blur(16px);
  border: 1px solid rgba(255, 255, 255, 0.8);
  border-radius: 20px; padding: 20px;
  box-shadow: 0 20px 40px -10px rgba(0,0,0,0.15);
  z-index: 1000; animation: slideUp 0.3s ease;
}

@keyframes slideUp {
  from { opacity: 0; transform: translateY(20px); }
  to { opacity: 1; transform: translateY(0); }
}

.btn-close-card {
  position: absolute; top: 12px; right: 12px;
  background: transparent; border: none; color: #94a3b8; font-size: 18px; cursor: pointer;
}

.op-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 6px; }
.op-id { font-size: 14px; font-weight: 700; color: #6366f1; font-family: monospace; }
.op-price { font-size: 18px; font-weight: 700; color: #0f172a; }
.op-address { font-size: 14px; color: #475569; margin-bottom: 6px; }
.op-distance { font-size: 13px; color: #64748b; }

.btn-accept-map {
  width: 100%; padding: 12px; border-radius: 14px; border: none;
  background: #10b981; color: white; font-weight: 600; font-size: 15px;
  cursor: pointer; display: flex; align-items: center; justify-content: center; gap: 8px;
  transition: all 0.2s ease;
}
.btn-accept-map:hover { background: #059669; }

.btn-disabled-hint {
  background: #fffbebfb; border: 1px solid #fde68a; color: #b45309;
  padding: 10px 14px; border-radius: 12px; font-size: 13px; text-align: center;
}

/* Cluster list overlay */
.cluster-list-card { padding-top: 24px; }
.cluster-title { font-size: 16px; font-weight: 700; color: #0f172a; padding-right: 28px; }
.cluster-address { font-size: 13px; color: #64748b; margin: 2px 0 12px; }
.cluster-rows {
  display: flex; flex-direction: column; gap: 8px;
  max-height: 40vh; overflow-y: auto;
}
.cluster-row {
  display: flex; align-items: center; gap: 10px;
  width: 100%; text-align: left; cursor: pointer;
  background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 14px;
  padding: 10px 14px; transition: all 0.15s ease;
}
.cluster-row:hover { background: #eef2ff; border-color: #c7d2fe; }
.cr-id { font-size: 13px; font-weight: 700; color: #6366f1; font-family: monospace; }
.cr-price { font-size: 15px; font-weight: 700; color: #0f172a; margin-left: auto; }
.cr-badge {
  font-size: 11px; font-weight: 600; padding: 3px 8px; border-radius: 99px; white-space: nowrap;
}
.cr-badge.ok { background: #dcfce7; color: #15803d; }
.cr-badge.far { background: #fef3c7; color: #b45309; }
.cr-arrow { color: #94a3b8; font-size: 14px; }

.btn-back-cluster {
  display: inline-flex; align-items: center; gap: 6px;
  background: transparent; border: none; color: #6366f1;
  font-size: 13px; font-weight: 600; cursor: pointer;
  padding: 0; margin-bottom: 10px;
}
</style>

<style>
.leaflet-control-attribution {
  display: none !important;
}

/* Leaflet Custom Marker Pins.
   Leaflet injects divIcon HTML outside Vue's scoped DOM, so these selectors
   must pierce scoping with :deep() — otherwise the marker backgrounds and the
   cluster's distinct colour never render. */
:deep(.user-pin-pulse) {
  width: 32px; height: 32px; border-radius: 50%;
  background: #6366f1; color: white;
  display: flex; align-items: center; justify-content: center; font-size: 16px;
  box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.3);
}

:deep(.order-pin-bubble) {
  padding: 4px 10px; border-radius: 99px; font-size: 12px; font-weight: 700;
  color: white; white-space: nowrap; text-align: center;
  box-shadow: 0 4px 12px rgba(0,0,0,0.15);
}
:deep(.order-pin-bubble.green) { background: #10b981; }
:deep(.order-pin-bubble.orange) { background: #f59e0b; }

/* Cluster marker: several stacked orders at one point */
:deep(.order-cluster-bubble) {
  padding: 5px 12px; border-radius: 99px; font-size: 12px; font-weight: 700;
  color: white; white-space: nowrap; text-align: center;
  background: #7c3aed;
  box-shadow: 0 4px 12px rgba(124, 58, 237, 0.35);
  border: 2px solid #ffffff;
  display: inline-flex; align-items: center; gap: 5px;
}
:deep(.order-cluster-bubble.has-accept) { background: #4f46e5; }
:deep(.order-cluster-bubble .cluster-count) {
  background: rgba(255, 255, 255, 0.25);
  border-radius: 99px; padding: 0 7px; font-size: 13px; font-weight: 800;
}
</style>
