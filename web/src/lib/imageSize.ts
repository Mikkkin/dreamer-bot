// The server keeps the same rule for its "full" variant: scale down only when
// wider than 1600 px or larger than 12 MP, keeping the aspect ratio, so tall
// recipe screenshots keep their full width and stay readable.

export const MAX_WIDTH = 1600
export const MAX_PIXELS = 12_000_000

export interface FittedSize {
  width: number
  height: number
  scaled: boolean
}

export function fitImage(width: number, height: number, maxWidth = MAX_WIDTH, maxPixels = MAX_PIXELS): FittedSize {
  if (!(width > 0 && height > 0)) throw new RangeError('image dimensions must be positive')
  if (width <= maxWidth && width * height <= maxPixels) return { width, height, scaled: false }

  const scale = Math.min(maxWidth / width, Math.sqrt(maxPixels / (width * height)))
  let w = Math.max(1, Math.min(maxWidth, Math.round(width * scale)))
  let h = Math.max(1, Math.round((w * height) / width))
  // Rounding may overshoot the pixel budget by a row or two; shrink until it fits.
  while (w > 1 && w * h > maxPixels) {
    w -= 1
    h = Math.max(1, Math.round((w * height) / width))
  }
  return { width: w, height: h, scaled: true }
}
