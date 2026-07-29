<template>
  <ServerStatusIndicator />
  <router-view />
</template>

<script lang="ts">
import { defineComponent } from 'vue'
import ServerStatusIndicator from './components/ServerStatusIndicator.vue'

export default defineComponent({
  name: 'App',
  components: { ServerStatusIndicator },
})
</script>

<style>
body {
  margin: 0;
  font-family: 'Source Sans Pro', sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}
</style>
