<template>
  <div v-if="show" class="map-modal-overlay" @click.self="show = false">
    <div class="map-modal-card">
      <div class="map-modal-header">
        <div>
          <h3 class="map-modal-title">Карта заказов (Зона 10 км)</h3>
          <p class="map-modal-subtitle">Зеленый круг (2 км) — зона мгновенного взятия заказа</p>
        </div>
        <button type="button" class="btn-close" aria-label="Закрыть" @click="show = false">
          <i class="ph ph-x"></i>
        </button>
      </div>

      <div class="map-modal-body">
        <div id="executor-leaflet-map" class="leaflet-container-box"></div>

        <!-- Selected Order Overlay Card -->
        <div v-if="selectedOrder" class="order-preview-card">
          <button type="button" class="btn-close-card" @click="selectedOrder = null">
            <i class="ph ph-x"></i>
          </button>
          <div class="op-header">
            <span class="op-id">#{{ selectedOrder.id.slice(0, 8) }}</span>
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
import { defineComponent, ref, computed, watch, onMounted, nextTick } from 'vue'
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
    const accepting = ref(false)
    let map: L.Map | null = null
    let markersLayer: L.LayerGroup | null = null
    let userMarker: L.Marker | null = null
    let zone2kmCircle: L.Circle | null = null
    let zone50kmCircle: L.Circle | null = null

    const mapOrders = ref<any[]>([])

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

        map = L.map(container).setView([props.currentLat, props.currentLon], 12)

        L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
          maxZoom: 19,
          attribution: '© OpenStreetMap',
        }).addTo(map)

        // 10km Outer Circle
        zone50kmCircle = L.circle([props.currentLat, props.currentLon], {
          radius: 10000,
          color: '#6366f1',
          weight: 1,
          dashArray: '6, 6',
          fillColor: '#6366f1',
          fillOpacity: 0.03,
        }).addTo(map)

        // 2km Accept Circle
        zone2kmCircle = L.circle([props.currentLat, props.currentLon], {
          radius: 2000,
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

        userMarker = L.marker([props.currentLat, props.currentLon], {
          draggable: true,
          icon: userIcon,
        }).addTo(map)

        userMarker.on('dragend', async (event: any) => {
          const position = event.target.getLatLng()
          await handleManualLocationChange(position.lat, position.lng)
        })

        markersLayer = L.layerGroup().addTo(map)
        fetchMapOrders()
      })
    }

    const handleManualLocationChange = async (lat: number, lon: number) => {
      try {
        const res = await api.post('/executor/set-location', {
          lat,
          lon,
          is_manual: true,
        })
        if (res.data && res.data.success) {
          emit('location-changed', { lat, lon })
          if (zone2kmCircle) zone2kmCircle.setLatLng([lat, lon])
          if (zone50kmCircle) zone50kmCircle.setLatLng([lat, lon])
          fetchMapOrders()
        }
      } catch (err: any) {
        alert(err.response?.data?.message || err.response?.data || 'Ошибка изменения метки (10 мин кулдаун)')
        if (userMarker) {
          userMarker.setLatLng([props.currentLat, props.currentLon])
        }
      }
    }

    const renderMarkers = () => {
      if (!markersLayer || !map) return
      markersLayer.clearLayers()

      mapOrders.value.forEach((order) => {
        const oLat = order.pickup_lat || 55.7558
        const oLon = order.pickup_lon || 37.6173
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
          selectedOrder.value = order
        })
        markersLayer?.addLayer(marker)
      })
    }

    const acceptMapOrder = async () => {
      if (!selectedOrder.value || accepting.value) return
      accepting.value = true
      try {
        await api.post(`/executor/orders/${selectedOrder.value.id}/accept`)
        emit('order-accepted', selectedOrder.value.id)
        selectedOrder.value = null
        show.value = false
      } catch (err: any) {
        alert(err.response?.data || 'Ошибка принятия заказа')
      } finally {
        accepting.value = false
      }
    }

    watch(show, (val) => {
      if (val) initMap()
    })

    return {
      show,
      selectedOrder,
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
</style>

<style>
/* Leaflet Custom Marker Pins */
.user-pin-pulse {
  width: 32px; height: 32px; border-radius: 50%;
  background: #6366f1; color: white;
  display: flex; align-items: center; justify-content: center; font-size: 16px;
  box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.3);
}

.order-pin-bubble {
  padding: 4px 10px; border-radius: 99px; font-size: 12px; font-weight: 700;
  color: white; white-space: nowrap; text-align: center;
  box-shadow: 0 4px 12px rgba(0,0,0,0.15);
}
.order-pin-bubble.green { background: #10b981; }
.order-pin-bubble.orange { background: #f59e0b; }
</style>
