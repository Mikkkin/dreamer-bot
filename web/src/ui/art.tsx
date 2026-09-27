import { useId, type CSSProperties, type ReactNode } from 'react'
import { cx } from '../lib/cx'
import type { Backdrop as BackdropColors } from '../lib/categoryStyle'
import { IconSparkles } from './icons'

/** An emoji drawn like a Telegram sticker: white outline, soft shadow, slight tilt. */
export function Sticker({ emoji, size = 56, tilt = -6, className }: { emoji: string; size?: number; tilt?: number; className?: string }) {
  const style = { '--s': `${size}px`, '--tilt': `${tilt}deg` } as CSSProperties
  return (
    <span className={cx('sticker', className)} style={style} aria-hidden="true">
      {emoji}
    </span>
  )
}

/** A deterministic −6…6° tilt, so each cover leans its own way but never jumps between renders. */
export function tiltFor(seed: number): number {
  return ((Math.abs(seed) % 5) - 2) * 3
}

const RING: Record<'card' | 'hero', number> = { card: 6, hero: 8 }
const STICKER: Record<'card' | 'hero' | 'tile', number> = { card: 56, hero: 104, tile: 22 }

interface BackdropProps extends BackdropColors {
  emoji: string
  size?: 'card' | 'hero' | 'tile'
  seed?: number
  /** Overrides the sticker size of the preset. */
  stickerSize?: number
  className?: string
}

/**
 * Cover art for items without photos, after Telegram's collectible gifts:
 * a radial two-tone backdrop, a faint ring of white symbols and a sticker.
 */
export function Backdrop({ emoji, center, edge, size = 'card', seed = 0, stickerSize, className }: BackdropProps) {
  const style = { '--bd-c': center, '--bd-e': edge } as CSSProperties
  return (
    <span className={cx('backdrop', `backdrop--${size}`, className)} style={style} aria-hidden="true">
      {size !== 'tile' && (
        <span className="backdrop__ring">
          {Array.from({ length: RING[size] }, (_, k) => (
            <span key={k} style={{ '--k': k } as CSSProperties}>
              {emoji}
            </span>
          ))}
        </span>
      )}
      <Sticker emoji={emoji} size={stickerSize ?? STICKER[size]} tilt={size === 'tile' ? 0 : tiltFor(seed)} />
    </span>
  )
}

interface DonutRingProps {
  /** 0..1 */
  value: number
  size?: number
  stroke?: number
  label: string
  children?: ReactNode
  className?: string
}

/** A progress ring drawn with the duo gradient ("done together"). */
export function DonutRing({ value, size = 48, stroke = 5, label, children, className }: DonutRingProps) {
  const id = `duo-${useId().replace(/[^\w-]/g, '')}`
  const r = (size - stroke) / 2
  const c = 2 * Math.PI * r
  const v = Math.min(1, Math.max(0, value))
  const style = { '--c': `${c}px`, width: size, height: size } as CSSProperties
  return (
    <span className={cx('donut', className)} style={style} role="img" aria-label={label}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden="true" focusable="false">
        <defs>
          <linearGradient id={id} x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" style={{ stopColor: 'var(--duo-a)' }} />
            <stop offset="1" style={{ stopColor: 'var(--duo-b)' }} />
          </linearGradient>
        </defs>
        <circle className="donut__track" cx={size / 2} cy={size / 2} r={r} strokeWidth={stroke} />
        {v > 0 && (
          <circle
            className="donut__value"
            cx={size / 2}
            cy={size / 2}
            r={r}
            strokeWidth={stroke}
            stroke={`url(#${id})`}
            strokeDasharray={`${c}px`}
            strokeDashoffset={`${c * (1 - v)}px`}
            transform={`rotate(-90 ${size / 2} ${size / 2})`}
          />
        )}
      </svg>
      {children && <span className="donut__center">{children}</span>}
    </span>
  )
}

/** Three twinkling sparkles around empty-state art. */
export function Sparkles() {
  return (
    <>
      <span className="spark spark--1" style={{ '--k': 0 } as CSSProperties}>
        <IconSparkles size={16} />
      </span>
      <span className="spark spark--2" style={{ '--k': 1 } as CSSProperties}>
        <IconSparkles size={20} />
      </span>
      <span className="spark spark--3" style={{ '--k': 2 } as CSSProperties}>
        <IconSparkles size={13} />
      </span>
    </>
  )
}
