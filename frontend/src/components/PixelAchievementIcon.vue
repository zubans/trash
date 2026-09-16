<template>
  <svg
    class="pixel-icon"
    :class="{ locked }"
    :style="{ width: size + 'px', height: size + 'px' }"
    viewBox="0 0 16 16"
    shape-rendering="crispEdges"
    role="img"
    :aria-label="title || name"
  >
    <title v-if="title">{{ title }}</title>
    <rect
      v-for="pixel in pixels"
      :key="pixel.key"
      :x="pixel.x"
      :y="pixel.y"
      width="1"
      height="1"
      :fill="pixel.fill"
    />
  </svg>
</template>

<script lang="ts">
import { computed, defineComponent } from 'vue'

// Пиксельные значки ачивок.
//
// Значок рисуется здесь, а не приезжает картинкой, по двум причинам. Скрипт
// ачивки называет иконку словом ("trophy"), и он не должен знать ни про файлы,
// ни про размеры; а пиксельная сетка 16×16 — единственная графика, которая
// одинаково резка и в списке, и на полке, и на экране любой плотности, потому
// что масштабируется целыми клетками.
//
// Спрайт — это шестнадцать строк по шестнадцать символов. Символ — ключ
// палитры, точка — прозрачная клетка.

interface Sprite {
  palette: Record<string, string>
  rows: string[]
}

// Кубок: первый заказ. Самая обычная ачивка и потому самый узнаваемый значок.
const TROPHY: Sprite = {
  palette: { d: '#8a5a10', g: '#f4b93c', l: '#ffe6a0', s: '#a9711a' },
  rows: [
    '................',
    '...dddddddddd...',
    '...dlgggggggd...',
    '.dddlgggggggddd.',
    '.d.dlgggggggd.d.',
    '.d.dlgggggggd.d.',
    '.dddlgggggggddd.',
    '...dlgggggggd...',
    '....dggggggd....',
    '.....dggggd.....',
    '.......dd.......',
    '......dggd......',
    '.....dggggd.....',
    '....dddddddd....',
    '...dssssssssd...',
    '...dddddddddd...',
  ],
}

// Револьвер: самый быстрый выстрел. Ствол смотрит вправо — в ту же сторону, в
// какую читается карточка.
const REVOLVER: Sprite = {
  palette: { M: '#475569', m: '#a3b1c2', b: '#8a5a2b', B: '#5b3517' },
  rows: [
    '................',
    '................',
    '................',
    '..MM............',
    '..MMMMMMMMMMMMM.',
    '..MmmmMmmmmmmmM.',
    '..MmmmMmmmmmmmM.',
    '..MmmmMMMMMMMMM.',
    '..MmmmM.........',
    '..MMbbMM........',
    '..Mbbb.M........',
    '..Mbbb.M........',
    '..MbbbMM........',
    '..MbbB..........',
    '..MbbB..........',
    '..MBBB..........',
  ],
}

// Медаль на ленте: марафон месяца. Лента отличает её от кубка на расстоянии, с
// которого форма ещё видна, а детали уже нет.
const MEDAL: Sprite = {
  palette: { r: '#ef4444', R: '#991b1b', g: '#f4b93c', d: '#8a5a10', l: '#ffe6a0' },
  rows: [
    '..RR........RR..',
    '..rR........Rr..',
    '...RR......RR...',
    '...rR......Rr...',
    '....RR....RR....',
    '....rRddddRr....',
    '.....dggggd.....',
    '....dggllggd....',
    '...dgggllgggd...',
    '...dggllllggd...',
    '...dgggllgggd...',
    '....dggllggd....',
    '.....dggggd.....',
    '......dddd......',
    '................',
    '................',
  ],
}

const STAR: Sprite = {
  palette: { y: '#fbbf24', Y: '#b45309', l: '#fef3c7' },
  rows: [
    '................',
    '.......yy.......',
    '.......yy.......',
    '......ylly......',
    '......ylly......',
    'YyyyyyllllyyyyyY',
    '.YyyyyllllyyyyY.',
    '..YyyyllllyyyY..',
    '...YyyllllyyY...',
    '..YyyyyllyyyyY..',
    '..Yyyyy..yyyyY..',
    '.Yyyy......yyyY.',
    '.Yyy........yyY.',
    '.YY..........YY.',
    '................',
    '................',
  ],
}

const FIRE: Sprite = {
  palette: { r: '#f97316', R: '#b91c1c', o: '#fb923c', y: '#fde047' },
  rows: [
    '................',
    '.......r........',
    '......rrr.......',
    '......rrrr......',
    '.....rrorrr.....',
    '.....rroorr.....',
    '....rroooorr....',
    '...rroooooorr...',
    '...rrooyyoorr...',
    '..rrooyyyyoorr..',
    '..rroyyyyyyorr..',
    '..rroyyyyyyorr..',
    '..RroyyyyyyorR..',
    '...Rrooyyoorr...',
    '....RRroorRR....',
    '................',
  ],
}

// Рукопожатие: первое покаяние — исполнитель признал оспоренный заказ.
const HANDSHAKE: Sprite = {
  palette: { b: '#2563eb', g: '#16a34a', o: '#7c2d12', s: '#f6c28b', d: '#d9925a' },
  rows: [
    '................',
    '................',
    '................',
    'bbb.........ggg.',
    'bbbooooo.oooggg.',
    'bbbssssosssoggg.',
    'bbbsdsssssssggg.',
    'bbbssdsdsdssggg.',
    'bbbosssssssoggg.',
    'bbb.ooosooo.ggg.',
    'bbb....oo...ggg.',
    '................',
    '................',
    '................',
    '................',
    '................',
  ],
}

// Печать с галочкой — значок ачивки, чью иконку скрипт не назвал или назвал
// незнакомым словом. Пустого места на полке быть не должно: значок есть, и
// выглядеть он обязан значком.
const SEAL: Sprite = {
  palette: { B: '#1d4ed8', b: '#60a5fa', w: '#eff6ff' },
  rows: [
    '................',
    '.....BBBBBB.....',
    '...BBbbbbbbBB...',
    '..BbbbbbbbbbbB..',
    '.BbbbbbbbbbbbbB.',
    '.BbbbbbbbbbwwbB.',
    '.BbbbbbbbbwwbbB.',
    '.BbwwbbbbwwbbbB.',
    '.BbbwwbbwwbbbbB.',
    '.BbbbwwwwbbbbbB.',
    '.BbbbbwwbbbbbbB.',
    '.BbbbbbbbbbbbbB.',
    '..BbbbbbbbbbbB..',
    '...BBbbbbbbBB...',
    '.....BBBBBB.....',
    '................',
  ],
}

const SPRITES: Record<string, Sprite> = {
  trophy: TROPHY,
  revolver: REVOLVER,
  medal: MEDAL,
  star: STAR,
  fire: FIRE,
  handshake: HANDSHAKE,
  seal: SEAL,
}

export default defineComponent({
  name: 'PixelAchievementIcon',
  props: {
    // Имя из манифеста ачивки. Незнакомое рисуется печатью, а не пустотой.
    name: { type: String, default: '' },
    // Значок ещё не получен: та же графика, но обесцвеченная. Разными
    // картинками полученное и неполученное различать нельзя — человек не должен
    // гадать, тот ли это значок, который он ищет.
    locked: { type: Boolean, default: false },
    size: { type: Number, default: 48 },
    title: { type: String, default: '' },
  },
  setup(props) {
    const pixels = computed(() => {
      const sprite = SPRITES[props.name] ?? SPRITES.seal
      const out: { key: string; x: number; y: number; fill: string }[] = []
      sprite.rows.forEach((row, y) => {
        for (let x = 0; x < row.length; x += 1) {
          const fill = sprite.palette[row[x]]
          if (fill) out.push({ key: `${x}:${y}`, x, y, fill })
        }
      })
      return out
    })

    return { pixels }
  },
})
</script>

<style scoped>
.pixel-icon {
  display: block;
  flex-shrink: 0;
  image-rendering: pixelated;
}

.pixel-icon.locked {
  filter: grayscale(1) opacity(0.4);
}
</style>
