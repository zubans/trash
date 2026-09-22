import { describe, it, expect } from 'vitest'
import { parseOffer, sectionAnchor } from './offerMarkdown'
import offer from '../content/shop-offer.md?raw'

describe('offer markdown', () => {
  it('reads headings, bold and joins wrapped lines into paragraphs', () => {
    const blocks = parseOffer('## 8. Возврат\n\n8.1. **Только** через\nчат.')
    expect(blocks).toEqual([
      { type: 'heading', html: '8. Возврат' },
      { type: 'paragraph', html: '8.1. <strong>Только</strong> через чат.' },
    ])
    expect(sectionAnchor(blocks[0])).toBe('section-8')
  })

  it('escapes markup instead of rendering it', () => {
    expect(parseOffer('<script>x</script>')[0].html).toBe('&lt;script&gt;x&lt;/script&gt;')
  })

  it('publishes only the public part, with the refund section', () => {
    expect(offer).not.toContain('Служебная часть')
    expect(parseOffer(offer).some((b) => sectionAnchor(b) === 'section-8')).toBe(true)
  })
})
