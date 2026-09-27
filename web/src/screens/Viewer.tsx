import { useEffect, useRef } from 'react'
import type { ApiImage } from '../api/types'
import { useIsTopScreen } from '../state/screen'
import { useTelegram } from '../telegram/hooks'
import { Img } from '../ui/media'

/**
 * Full images at container width with vertical scrolling, so a tall recipe
 * screenshot is readable top to bottom. The BackButton closes it. Like any
 * media viewer it is black regardless of the theme, Telegram chrome included.
 */
export function Viewer({ images, start, title }: { images: ApiImage[]; start: number; title: string }) {
  const tg = useTelegram()
  const isTop = useIsTopScreen()
  const items = useRef<(HTMLElement | null)[]>([])

  // A passive effect runs after the navigator has reset the scroll position.
  useEffect(() => {
    if (start > 0) items.current[start]?.scrollIntoView({ block: 'start' })
  }, [start])

  useEffect(() => {
    if (!isTop) return
    tg.setChromeColor('#000000')
    return () => tg.setChromeColor(null)
  }, [tg, isTop])

  return (
    <div className="viewer">
      {images.map((img, i) => (
        <figure
          key={img.id}
          ref={(el) => {
            items.current[i] = el
          }}
          className="viewer__item"
          style={{ aspectRatio: `${img.width} / ${img.height}` }}
        >
          <Img src={img.full_url} alt={images.length > 1 ? `${title} — ${i + 1} из ${images.length}` : title} eager={Math.abs(i - start) <= 1} />
          {images.length > 1 && (
            <figcaption className="viewer__index num" aria-hidden="true">
              {i + 1}/{images.length}
            </figcaption>
          )}
        </figure>
      ))}
    </div>
  )
}
