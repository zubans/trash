// Текст оферты хранится маркдауном (src/content/shop-offer.md) — в том виде,
// в каком его правит юрист. Разметки в нём три вида: заголовки «## », жирный
// «**…**» и абзацы; этого разборщика хватает ровно на них, и библиотека ради
// одной страницы не нужна.

export interface OfferBlock {
  type: 'heading' | 'paragraph'
  html: string
}

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function inline(text: string): string {
  return escapeHtml(text).replace(/\*\*(.+?)\*\*/gs, '<strong>$1</strong>')
}

export function parseOffer(markdown: string): OfferBlock[] {
  return markdown
    .split(/\n\s*\n/)
    .map((chunk) => chunk.trim())
    .filter(Boolean)
    .map((chunk) => {
      if (chunk.startsWith('## ')) return { type: 'heading', html: inline(chunk.slice(3).trim()) }
      return { type: 'paragraph', html: inline(chunk.replace(/\s*\n\s*/g, ' ')) }
    })
}

// Якорь раздела: «## 8. Возврат…» → «section-8», чтобы ссылка «п. 8 оферты»
// открывала страницу сразу на нём.
export function sectionAnchor(block: OfferBlock): string | undefined {
  if (block.type !== 'heading') return undefined
  const match = block.html.match(/^(\d+)\./)
  return match ? `section-${match[1]}` : undefined
}
