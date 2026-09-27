import { useRef, useState, type CSSProperties, type ReactNode } from 'react'
import type { ApiImage } from '../api/types'
import { cx } from '../lib/cx'
import { IconExpand, IconHeart } from './icons'

interface ImgProps {
  /** A server-signed URL (thumb_url / full_url) or a local blob: preview. */
  src: string
  alt: string
  /** Anchor the crop to the top: the start of a screenshot is its most useful part. */
  top?: boolean
  eager?: boolean
}

/** An image that fades in once decoded and disappears on error, revealing the fallback behind it. */
export function Img({ src, alt, top, eager }: ImgProps) {
  const [state, setState] = useState<'loading' | 'loaded' | 'error'>('loading')
  return (
    <img
      src={src}
      alt={alt}
      loading={eager ? 'eager' : 'lazy'}
      decoding="async"
      draggable={false}
      className={cx('img', top && 'img--top')}
      data-state={state}
      onLoad={() => setState('loaded')}
      onError={() => setState('error')}
    />
  )
}

/** Initials only: Telegram profile photos live on an origin the CSP does not allow. */
export function Avatar({ name, slot, size = 22 }: { name: string; slot: 'a' | 'b'; size?: number }) {
  const initial = [...name.trim()][0]?.toLocaleUpperCase('ru') ?? '?'
  const style = { '--av': `${size}px` } as CSSProperties
  return (
    <span className={cx('avatar', `avatar--${slot}`)} style={style} role="img" aria-label={name} title={name}>
      {initial}
    </span>
  )
}

/** The couple, overlapping. With one person, a quiet heart keeps the second seat. */
export function PairAvatars({ people, size = 28 }: { people: readonly { name: string; slot: 'a' | 'b' }[]; size?: number }) {
  const style = { '--av': `${size}px` } as CSSProperties
  return (
    <span className="pair" style={style}>
      {people.slice(0, 2).map((p) => (
        <Avatar key={p.slot + p.name} name={p.name} slot={p.slot} size={size} />
      ))}
      {people.length < 2 && (
        <span className="avatar avatar--ghost" aria-hidden="true">
          <IconHeart size={Math.round(size * 0.5)} strokeWidth={2.2} />
        </span>
      )}
    </span>
  )
}

const SLIDE_GAP = 8

interface CarouselProps {
  images: ApiImage[]
  alt: string
  /** Behind each slide while it loads. */
  background: string
  /** The edge colour of the item's art, for the soft glow under the hero. */
  glow?: string
  top?: boolean
  /** Frame light screenshots with a hairline. */
  framed?: boolean
  onOpen: (index: number) => void
  /** Shown when there are no images. */
  fallback: ReactNode
}

/**
 * Horizontal photo carousel with 16 px side insets (so it does not fight the
 * iOS edge swipe), a counter, an "open full" button and dots.
 */
export function Carousel({ images, alt, background, glow, top, framed, onOpen, fallback }: CarouselProps) {
  const track = useRef<HTMLDivElement>(null)
  const [index, setIndex] = useState(0)
  const n = images.length
  const style = glow ? ({ '--bd-e': glow } as CSSProperties) : undefined

  if (n === 0) {
    return (
      <div className="carousel carousel--empty" style={style}>
        {fallback}
      </div>
    )
  }

  const onScroll = () => {
    const el = track.current
    const first = el?.firstElementChild
    if (!el || !(first instanceof HTMLElement)) return
    const step = first.offsetWidth + SLIDE_GAP
    setIndex(Math.min(n - 1, Math.max(0, Math.round(el.scrollLeft / step))))
  }
  const current = Math.min(index, n - 1)

  return (
    <div className={cx('carousel', n === 1 && 'carousel--single', framed && 'carousel--framed')} style={style}>
      <div className="carousel__track" ref={track} onScroll={onScroll}>
        {images.map((img, i) => (
          <button
            key={img.id}
            type="button"
            className="carousel__slide"
            style={{ background }}
            onClick={() => onOpen(i)}
            aria-label={n > 1 ? `Открыть фото ${i + 1} из ${n}` : 'Открыть фото'}
          >
            <Img src={img.full_url} alt={i === 0 ? alt : `${alt} — фото ${i + 1}`} top={top} eager={i === 0} />
          </button>
        ))}
      </div>
      {/* Sized like the slide, so the controls sit on the visible photo. */}
      <div className="carousel__overlay">
        {n > 1 && (
          <span className="carousel__counter num" aria-hidden="true">
            {current + 1}/{n}
          </span>
        )}
        <button type="button" className="carousel__expand" aria-label="Открыть фото целиком" onClick={() => onOpen(current)}>
          <span>
            <IconExpand size={16} strokeWidth={2.2} />
          </span>
        </button>
      </div>
      {n > 1 && (
        <div className="carousel__dots" aria-hidden="true">
          {images.map((img, i) => (
            <span key={img.id} className={cx('dot', i === current && 'dot--active')} />
          ))}
        </div>
      )}
    </div>
  )
}
