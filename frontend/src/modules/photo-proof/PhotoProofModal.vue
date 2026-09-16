<template>
  <div class="proof-overlay" @click.self="$emit('close')">
    <div class="proof-modal" role="dialog" aria-modal="true">
      <header class="proof-header">
        <div>
          <h3>Фото-подтверждение</h3>
          <p class="proof-subtitle">
            Этот заказ закрывается только с фотографией места заказа и жестом рядом с объектом.
          </p>
        </div>
        <button type="button" class="proof-close" aria-label="Закрыть" @click="$emit('close')">
          <i class="ph-bold ph-x"></i>
        </button>
      </header>

      <div class="proof-body">
        <div v-if="gesture" class="proof-gesture">
          <i class="ph-fill ph-hand-peace"></i>
          <div>
            <div class="proof-gesture-title">Жест: {{ gesture.title }}</div>
            <div class="proof-gesture-text">{{ gesture.description }}</div>
          </div>
        </div>

        <div class="proof-camera">
          <span class="proof-label">Камера</span>
          <div class="proof-segment">
            <button type="button" :class="{ active: camera === 'REAR' }" @click="camera = 'REAR'">
              <i class="ph-bold ph-camera"></i> Основная
            </button>
            <button type="button" :class="{ active: camera === 'FRONT' }" @click="camera = 'FRONT'">
              <i class="ph-bold ph-camera-rotate"></i> Фронтальная
            </button>
          </div>
        </div>

        <div class="proof-shot">
          <div class="proof-shot-head">
            <span class="proof-label">Место заказа <span class="proof-required">обязательно</span></span>
            <span v-if="shots.AREA" class="proof-saved"><i class="ph-bold ph-check"></i> снято</span>
          </div>
          <img v-if="shots.AREA?.preview" :src="shots.AREA.preview" alt="Снимок места заказа" class="proof-preview" />
          <button type="button" class="proof-btn primary" :disabled="busy" @click="askHint('AREA')">
            <i class="ph-bold ph-camera me-1"></i>
            {{ shots.AREA ? 'Переснять место заказа' : 'Фотографировать место заказа' }}
          </button>
        </div>

        <div class="proof-shot">
          <label class="proof-consent">
            <input v-model="selfieConsent" type="checkbox" />
            Заказчик согласен на совместное селфи
          </label>
          <template v-if="selfieConsent">
            <img v-if="shots.SELFIE?.preview" :src="shots.SELFIE.preview" alt="Селфи с заказчиком" class="proof-preview" />
            <button type="button" class="proof-btn secondary" :disabled="busy" @click="askHint('SELFIE')">
              <i class="ph-bold ph-user-focus me-1"></i>
              {{ shots.SELFIE ? 'Переснять селфи' : 'Сделать селфи (необязательно)' }}
            </button>
          </template>
        </div>

        <p v-if="busy" class="proof-status"><span class="spinner-sm dark"></span> Готовим снимок…</p>
        <p v-if="errorText" class="proof-error">{{ errorText }}</p>
        <p v-if="!online" class="proof-offline">
          <i class="ph-bold ph-wifi-slash"></i> Сети нет: снимки и отметка сохранятся на телефоне и отправятся сами, когда сеть появится.
        </p>
      </div>

      <footer class="proof-footer">
        <button type="button" class="proof-btn secondary" @click="$emit('close')">Позже</button>
        <button type="button" class="proof-btn success" :disabled="!shots.AREA || busy || finishing" @click="finish">
          <i class="ph-bold ph-check me-1"></i> Отметить исполненным
        </button>
      </footer>

      <input
        ref="fileInput"
        type="file"
        accept="image/jpeg"
        :capture="camera === 'FRONT' ? 'user' : 'environment'"
        style="display: none"
        @change="onFileChosen"
      />
    </div>

    <WatermarkHintPopup
      v-if="hintFor && gesture"
      :gesture="gesture"
      :selfie="hintFor === 'SELFIE'"
      @cancel="hintFor = null"
      @confirm="startCapture"
    />
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onUnmounted, reactive, ref, type PropType } from 'vue'
import { Capacitor } from '@capacitor/core'
import { Camera, CameraDirection, CameraResultType, CameraSource } from '@capacitor/camera'
import WatermarkHintPopup, { type Gesture } from './WatermarkHintPopup.vue'
import { proofQueue } from './queue'
import { online } from './network'
import { base64ToBytes, coordText, prepareProofPhoto, toRfc3339 } from './imageSync'
import { getCurrentCoordinates } from '../../services/geolocation'

type ShotKind = 'AREA' | 'SELFIE'

interface Shot {
  preview: string
}

// Сколько ждать координату для снимка. Снимок важнее точки: без неё он всё
// равно уходит, а сверка покажет, что координаты нет.
const COORDINATES_TIMEOUT_MS = 8000

function withTimeout<T>(promise: Promise<T>, ms: number): Promise<T | null> {
  return Promise.race([promise, new Promise<null>((resolve) => setTimeout(() => resolve(null), ms))])
}

export default defineComponent({
  name: 'PhotoProofModal',
  components: { WatermarkHintPopup },
  props: {
    order: { type: Object as PropType<any>, required: true },
  },
  emits: ['close', 'done'],
  setup(props, { emit }) {
    const queue = proofQueue()
    const camera = ref<'REAR' | 'FRONT'>('REAR')
    const selfieConsent = ref(false)
    const shots = reactive<Partial<Record<ShotKind, Shot>>>({})
    const hintFor = ref<ShotKind | null>(null)
    const capturing = ref<ShotKind | null>(null)
    const busy = ref(false)
    const finishing = ref(false)
    const errorText = ref('')
    const fileInput = ref<HTMLInputElement | null>(null)
    const objectUrls: string[] = []

    const gesture = computed<Gesture | null>(() => props.order?.photo_proof?.gesture || null)

    // Снимки, сделанные раньше и ещё не ушедшие (например, до перезапуска
    // приложения), засчитываются: переснимать их не нужно.
    for (const pending of queue.pendingPhotosFor(props.order.id)) {
      shots[pending.photoKind] = { preview: '' }
      if (pending.photoKind === 'SELFIE') selfieConsent.value = true
    }

    const askHint = (kind: ShotKind) => {
      errorText.value = ''
      if (kind === 'SELFIE' && camera.value === 'REAR') camera.value = 'FRONT'
      if (kind === 'AREA' && camera.value === 'FRONT' && !shots.AREA) camera.value = 'REAR'
      hintFor.value = kind
    }

    const readCameraPhoto = async (): Promise<Uint8Array | null> => {
      const photo = await Camera.getPhoto({
        quality: 92,
        resultType: CameraResultType.Uri,
        // Только камера: снимок из галереи ничего не доказывает.
        source: CameraSource.Camera,
        direction: camera.value === 'FRONT' ? CameraDirection.Front : CameraDirection.Rear,
        correctOrientation: false,
        saveToGallery: false,
      })
      if (!photo.webPath) return null
      const response = await fetch(photo.webPath)
      return new Uint8Array(await response.arrayBuffer())
    }

    const process = async (kind: ShotKind, original: Uint8Array, takenAt: Date) => {
      busy.value = true
      try {
        const coords = await withTimeout(getCurrentCoordinates().catch(() => null), COORDINATES_TIMEOUT_MS)
        const lat = coords ? Number(coords.lat.toFixed(6)) : null
        const lon = coords ? Number(coords.lon.toFixed(6)) : null
        const proof = props.order.photo_proof
        const prepared = await prepareProofPhoto(original, {
          orderId: props.order.id,
          symbolCode: proof.gesture.code,
          symbolNumber: proof.gesture.number,
          key: base64ToBytes(proof.nonce),
          takenAt,
          lat,
          lon,
        })
        await queue.enqueuePhoto(
          {
            orderId: props.order.id,
            photoKind: kind,
            camera: camera.value,
            takenAt: toRfc3339(takenAt),
            lat: lat === null ? null : coordText(lat),
            lon: lon === null ? null : coordText(lon),
            accuracy: null,
          },
          prepared,
        )
        const url = URL.createObjectURL(new Blob([prepared as BlobPart], { type: 'image/jpeg' }))
        objectUrls.push(url)
        shots[kind] = { preview: url }
        if (online.value) void queue.flush()
      } catch (err: any) {
        console.warn('[photo-proof] cannot prepare photo', err)
        errorText.value = 'Не удалось обработать снимок. Сделайте его ещё раз камерой телефона.'
      } finally {
        busy.value = false
      }
    }

    const startCapture = async () => {
      const kind = hintFor.value
      hintFor.value = null
      if (!kind) return
      // Время съёмки — момент нажатия, до обработки и до ожидания координаты.
      const takenAt = new Date()
      takenAt.setMilliseconds(0)

      if (!Capacitor.isNativePlatform()) {
        capturing.value = kind
        fileInput.value?.click()
        return
      }
      try {
        const original = await readCameraPhoto()
        if (original) await process(kind, original, takenAt)
      } catch (err) {
        // Отмена съёмки — не ошибка.
        console.warn('[photo-proof] camera closed', err)
      }
    }

    const onFileChosen = async (event: Event) => {
      const input = event.target as HTMLInputElement
      const file = input.files?.[0]
      const kind = capturing.value
      input.value = ''
      capturing.value = null
      if (!file || !kind) return
      const takenAt = new Date()
      takenAt.setMilliseconds(0)
      await process(kind, new Uint8Array(await file.arrayBuffer()), takenAt)
    }

    const finish = async () => {
      if (!shots.AREA) return
      finishing.value = true
      try {
        const now = new Date()
        now.setMilliseconds(0)
        queue.enqueueExecute(props.order.id, toRfc3339(now))
        if (online.value) await queue.flush()
        emit('done', { sent: !queue.pendingExecutions().has(props.order.id) })
      } finally {
        finishing.value = false
      }
    }

    onUnmounted(() => objectUrls.forEach((u) => URL.revokeObjectURL(u)))

    return {
      camera,
      selfieConsent,
      shots,
      hintFor,
      busy,
      finishing,
      errorText,
      fileInput,
      gesture,
      online,
      askHint,
      startCapture,
      onFileChosen,
      finish,
    }
  },
})
</script>

<style scoped>
.proof-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  z-index: 1200;
}
.proof-modal {
  background: #fff;
  border-radius: 20px;
  width: 100%;
  max-width: 440px;
  max-height: calc(100vh - 32px);
  overflow-y: auto;
  box-shadow: 0 24px 48px -16px rgba(15, 23, 42, 0.35);
}
.proof-header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 20px 20px 10px;
}
.proof-header h3 {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  color: #0f172a;
}
.proof-subtitle {
  margin: 6px 0 0;
  font-size: 13px;
  color: #64748b;
  line-height: 1.4;
}
.proof-close {
  border: none;
  background: #f1f5f9;
  color: #475569;
  width: 32px;
  height: 32px;
  border-radius: 10px;
  cursor: pointer;
  flex-shrink: 0;
}
.proof-body {
  padding: 4px 20px 8px;
}
.proof-gesture {
  display: flex;
  gap: 12px;
  background: #eef2ff;
  border-radius: 14px;
  padding: 12px;
  margin-bottom: 14px;
  color: #3730a3;
}
.proof-gesture i {
  font-size: 28px;
}
.proof-gesture-title {
  font-weight: 700;
  font-size: 14px;
}
.proof-gesture-text {
  font-size: 13px;
  line-height: 1.4;
  color: #4338ca;
}
.proof-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.4px;
  color: #64748b;
  text-transform: uppercase;
}
.proof-required {
  color: #b45309;
  text-transform: none;
  letter-spacing: 0;
  font-weight: 600;
}
.proof-camera {
  margin-bottom: 14px;
}
.proof-segment {
  display: flex;
  gap: 6px;
  margin-top: 6px;
}
.proof-segment button {
  flex: 1;
  height: 40px;
  border-radius: 10px;
  border: 1px solid #e2e8f0;
  background: #fff;
  color: #475569;
  font-weight: 600;
  cursor: pointer;
  font-family: inherit;
}
.proof-segment button.active {
  border-color: #4f46e5;
  background: #eef2ff;
  color: #4338ca;
}
.proof-shot {
  border-top: 1px solid #f1f5f9;
  padding: 12px 0;
}
.proof-shot-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}
.proof-saved {
  font-size: 12px;
  color: #047857;
  font-weight: 600;
}
.proof-preview {
  width: 100%;
  max-height: 200px;
  object-fit: cover;
  border-radius: 12px;
  margin-bottom: 8px;
}
.proof-consent {
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 14px;
  color: #334155;
  margin-bottom: 8px;
}
.proof-btn {
  width: 100%;
  height: 44px;
  border: none;
  border-radius: 12px;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  font-family: inherit;
}
.proof-btn:disabled {
  opacity: 0.55;
  cursor: default;
}
.proof-btn.primary {
  background: #4f46e5;
  color: #fff;
}
.proof-btn.secondary {
  background: #f1f5f9;
  color: #475569;
}
.proof-btn.success {
  background: #059669;
  color: #fff;
}
.proof-status,
.proof-error,
.proof-offline {
  font-size: 13px;
  margin: 8px 0 0;
  line-height: 1.4;
}
.proof-error {
  color: #b91c1c;
}
.proof-offline {
  color: #92400e;
  background: #fffbeb;
  padding: 8px 10px;
  border-radius: 10px;
}
.proof-footer {
  display: flex;
  gap: 10px;
  padding: 12px 20px 20px;
}
</style>
