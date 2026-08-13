<template>
  <va-modal
    v-model="show"
    title="🐞 Расширенная Диагностика Загрузки (Android)"
    hide-default-actions
    max-width="600px"
  >
    <div class="p-3 style-mono text-xs">
      <div class="mb-3 text-secondary">
        Этот инструмент тестирует прямой `fetch()` запрос к HTTP (8089) и HTTPS (8443) адресам сервера со стороны вашего Android WebView.
      </div>
      
      <va-button color="warning" size="small" class="mb-3" :loading="testingFetch" @click="$emit('runDiagnostics')">
        🔄 Перезапустить Тест Fetch
      </va-button>

      <!-- HTTP Status -->
      <div class="p-2 border rounded mb-3 bg-dark text-white">
        <div class="font-bold text-info mb-1">1. HTTP (8089)</div>
        <div>URL: http://94.103.9.172:8089/uploads/chat/029c51c0-3bc9-4569-b49c-6247839105d0_1786616908.jpg</div>
        <div v-if="httpFetchResult">
          Status: <strong :class="httpFetchResult.ok ? 'text-success' : 'text-danger'">{{ httpFetchResult.status }} {{ httpFetchResult.statusText }}</strong>
          <span class="ml-2" v-if="httpFetchResult.size">Size: {{ httpFetchResult.size }} bytes</span>
        </div>
        <div v-if="httpBlobUrl" class="mt-2 text-center bg-white p-2 rounded">
          <img :src="httpBlobUrl" style="max-height: 120px;" class="rounded border" alt="http test" />
          <div class="text-success font-bold mt-1 text-xxs">✅ Изображение успешно получено в Blob</div>
        </div>
      </div>

      <!-- HTTPS Status -->
      <div class="p-2 border rounded mb-3 bg-dark text-white">
        <div class="font-bold text-info mb-1">2. HTTPS (8443)</div>
        <div>URL: https://94.103.9.172:8443/uploads/chat/029c51c0-3bc9-4569-b49c-6247839105d0_1786616908.jpg</div>
        <div v-if="httpsFetchResult">
          Status: <strong :class="httpsFetchResult.ok ? 'text-success' : 'text-danger'">{{ httpsFetchResult.status }} {{ httpsFetchResult.statusText }}</strong>
          <span class="ml-2" v-if="httpsFetchResult.size">Size: {{ httpsFetchResult.size }} bytes</span>
          <div v-if="httpsFetchResult.error" class="text-danger mt-1">{{ httpsFetchResult.error }}</div>
        </div>
        <div v-if="httpsBlobUrl" class="mt-2 text-center bg-white p-2 rounded">
          <img :src="httpsBlobUrl" style="max-height: 120px;" class="rounded border" alt="https test" />
          <div class="text-success font-bold mt-1 text-xxs">✅ Изображение успешно получено в Blob</div>
        </div>
      </div>

      <div class="text-right">
        <va-button color="secondary" @click="show = false">
          {{ $t('common.close') }}
        </va-button>
      </div>
    </div>
  </va-modal>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue'

export default defineComponent({
  name: 'ImageDebugModal',
  props: {
    modelValue: { type: Boolean, required: true },
    testingFetch: { type: Boolean, default: false },
    httpFetchResult: { type: Object, default: null },
    httpsFetchResult: { type: Object, default: null },
    httpBlobUrl: { type: String, default: '' },
    httpsBlobUrl: { type: String, default: '' },
  },
  emits: ['update:modelValue', 'runDiagnostics'],
  setup(props, { emit }) {
    const show = computed({
      get: () => props.modelValue,
      set: (val) => emit('update:modelValue', val),
    })

    return { show }
  },
})
</script>
