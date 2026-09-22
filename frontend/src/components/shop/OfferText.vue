<template>
  <div class="offer-text">
    <template v-for="(block, i) in blocks" :key="i">
      <h3 v-if="block.type === 'heading'" :id="anchor(block)" class="offer-heading" v-html="block.html"></h3>
      <p v-else class="offer-paragraph" v-html="block.html"></p>
    </template>
  </div>
</template>

<script lang="ts">
import { defineComponent } from 'vue'
import offer from '../../content/shop-offer.md?raw'
import { parseOffer, sectionAnchor } from '../../utils/offerMarkdown'

// Текст оферты. Разбирается свой маркдаун с экранированием, поэтому v-html
// здесь безопасен: в разметку попадает только <strong>.
export default defineComponent({
  name: 'OfferText',
  setup() {
    return { blocks: parseOffer(offer), anchor: sectionAnchor }
  },
})
</script>

<style scoped>
.offer-text {
  font-size: 14px;
  line-height: 1.55;
  color: #334155;
}
.offer-heading {
  margin: 20px 0 8px;
  font-size: 16px;
  font-weight: 700;
  color: #0f172a;
  scroll-margin-top: 16px;
}
.offer-paragraph {
  margin: 0 0 10px;
}
</style>
