/**
 * Утилита для сжатия файлов изображений на стороне клиента перед загрузкой.
 * Целевой размер файла — от 150 КБ до 300 КБ.
 */
export async function compressImage(
  file: File,
  targetMinKB: number = 150,
  targetMaxKB: number = 300,
  maxDimension: number = 1600
): Promise<File> {
  // Если это не изображение или оно уже меньше targetMaxKB, возвращаем как есть
  if (!file.type.startsWith('image/')) {
    return file
  }
  const sizeKB = file.size / 1024
  if (sizeKB <= targetMaxKB && sizeKB >= targetMinKB) {
    return file
  }

  return new Promise((resolve) => {
    const reader = new FileReader()
    reader.onload = (e) => {
      const img = new Image()
      img.onload = () => {
        let width = img.width
        let height = img.height

        // Уменьшаем размеры изображения, если они больше maxDimension
        if (width > maxDimension || height > maxDimension) {
          if (width > height) {
            height = Math.round((height * maxDimension) / width)
            width = maxDimension
          } else {
            width = Math.round((width * maxDimension) / height)
            height = maxDimension
          }
        }

        const canvas = document.createElement('canvas')
        canvas.width = width
        canvas.height = height
        const ctx = canvas.getContext('2d')
        if (!ctx) {
          resolve(file)
          return
        }

        ctx.drawImage(img, 0, 0, width, height)

        // Выполняем двоичный поиск / итеративное сжатие по качеству (0.3 … 0.92)
        let minQuality = 0.3
        let maxQuality = 0.92

        const compressIteration = (quality: number, attemptsLeft: number) => {
          canvas.toBlob(
            (blob) => {
              if (!blob) {
                resolve(file)
                return
              }

              const currentSizeKB = blob.size / 1024

              if (attemptsLeft <= 0 || (currentSizeKB >= targetMinKB && currentSizeKB <= targetMaxKB)) {
                const compressedFile = new File(
                  [blob],
                  file.name.replace(/\.[^/.]+$/, '') + '.jpg',
                  { type: 'image/jpeg', lastModified: Date.now() }
                )
                resolve(compressedFile)
                return
              }

              if (currentSizeKB > targetMaxKB) {
                maxQuality = quality
              } else if (currentSizeKB < targetMinKB) {
                minQuality = quality
              }

              const nextQuality = (minQuality + maxQuality) / 2
              compressIteration(nextQuality, attemptsLeft - 1)
            },
            'image/jpeg',
            quality
          )
        }

        compressIteration(0.75, 6)
      }
      img.onerror = () => resolve(file)
      img.src = e.target?.result as string
    }
    reader.onerror = () => resolve(file)
    reader.readAsDataURL(file)
  })
}
