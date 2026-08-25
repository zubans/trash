/**
 * Utility to compress image files on the client side before uploading.
 * Targets a file size between 150 KB and 300 KB.
 */
export async function compressImage(
  file: File,
  targetMinKB: number = 150,
  targetMaxKB: number = 300,
  maxDimension: number = 1600
): Promise<File> {
  // If not an image or already smaller than targetMaxKB, return as-is
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

        // Downscale image dimensions if larger than maxDimension
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

        // Perform binary search / iterative compression for quality (0.3 to 0.92)
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
