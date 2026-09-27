import { fitImage } from '../lib/imageSize'

const JPEG_QUALITY = 0.85

async function decode(file: Blob): Promise<ImageBitmap | null> {
  try {
    return await createImageBitmap(file, { imageOrientation: 'from-image' })
  } catch {
    // Older WebViews reject the options bag; they still honour EXIF orientation by default.
  }
  try {
    return await createImageBitmap(file)
  } catch {
    return null
  }
}

/**
 * Re-encodes a picked photo as JPEG q0.85 using the server's size rule. This
 * shrinks uploads over slow tunnels, normalises HEIC on iOS and drops EXIF
 * (including GPS) before anything leaves the phone. When the browser cannot
 * decode the file it is sent as is and the server decides.
 */
export async function prepareUpload(file: Blob): Promise<Blob> {
  const bitmap = await decode(file)
  if (!bitmap) return file
  try {
    const { width, height } = fitImage(bitmap.width, bitmap.height)
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    if (!ctx) return file
    // JPEG has no alpha; flatten onto white like the server does.
    ctx.fillStyle = '#fff'
    ctx.fillRect(0, 0, width, height)
    ctx.imageSmoothingEnabled = true
    ctx.imageSmoothingQuality = 'high'
    ctx.drawImage(bitmap, 0, 0, width, height)
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/jpeg', JPEG_QUALITY))
    // Release the backing store right away; iOS WebViews have a tight canvas memory budget.
    canvas.width = 0
    canvas.height = 0
    return blob ?? file
  } catch {
    return file
  } finally {
    bitmap.close()
  }
}
