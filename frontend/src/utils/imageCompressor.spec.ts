import { describe, it, expect } from 'vitest'
import { compressImage } from './imageCompressor'

describe('compressImage', () => {
  it('returns non-image file unchanged', async () => {
    const textFile = new File(['hello world'], 'test.txt', { type: 'text/plain' })
    const result = await compressImage(textFile)
    expect(result).toBe(textFile)
  })

  it('returns image file unchanged if size is already within target bounds', async () => {
    // Создаём фиктивный файл размером около 200 КБ
    const content = new ArrayBuffer(200 * 1024)
    const imageFile = new File([content], 'test.jpg', { type: 'image/jpeg' })
    const result = await compressImage(imageFile, 150, 300)
    expect(result).toBe(imageFile)
  })
})
