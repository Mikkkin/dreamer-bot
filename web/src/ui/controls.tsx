import { useEffect, useId, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from 'react'
import type { Status } from '../api/types'
import { cx } from '../lib/cx'
import { useTelegram } from '../telegram/hooks'
import { IconClear, IconHeart, IconPiggy, IconSearch, IconSparkles } from './icons'

export interface SegmentOption<T extends string> {
  value: T
  label: ReactNode
  count?: number
  icon?: ReactNode
  /** Tints the icon of the selected segment (want / progress / done). */
  tone?: string
}

interface SegmentedProps<T extends string> {
  options: readonly SegmentOption<T>[]
  value: T
  onChange: (value: T) => void
  label: string
  className?: string
}

/**
 * Moves a roving selection with ←/→. Focus follows only keyboard moves, so a
 * tap never paints a focus ring around the control.
 */
function useRovingKeys(count: number, index: number, select: (i: number, fromKeyboard: boolean) => void) {
  return (e: KeyboardEvent) => {
    if (e.key === 'ArrowRight') select((index + 1) % count, true)
    else if (e.key === 'ArrowLeft') select((index - 1 + count) % count, true)
    else if (e.key === 'Home') select(0, true)
    else if (e.key === 'End') select(count - 1, true)
    else return
    e.preventDefault()
  }
}

/** A capsule segmented control with a sliding thumb (tablist semantics). */
export function Segmented<T extends string>({ options, value, onChange, label, className }: SegmentedProps<T>) {
  const tg = useTelegram()
  const refs = useRef<(HTMLButtonElement | null)[]>([])
  const index = Math.max(0, options.findIndex((o) => o.value === value))

  const select = (i: number, fromKeyboard: boolean) => {
    const option = options[i]
    if (!option) return
    if (option.value !== value) {
      tg.haptic.selection()
      onChange(option.value)
    }
    if (fromKeyboard) refs.current[i]?.focus()
  }
  const onKeyDown = useRovingKeys(options.length, index, select)

  const style = { '--i': index, '--n': options.length } as CSSProperties
  return (
    <div className={cx('seg', className)} role="tablist" aria-label={label} style={style} onKeyDown={onKeyDown}>
      <span className="seg__thumb" aria-hidden="true" />
      {options.map((o, i) => (
        <button
          key={o.value}
          ref={(el) => {
            refs.current[i] = el
          }}
          type="button"
          role="tab"
          aria-selected={i === index}
          tabIndex={i === index ? 0 : -1}
          className="seg__item"
          onClick={() => select(i, false)}
        >
          {o.icon && <span className={cx('seg__icon', o.tone && `seg__icon--${o.tone}`)}>{o.icon}</span>}
          <span className="seg__label">{o.label}</span>
          {o.count !== undefined && <span className="seg__count num">{o.count}</span>}
        </button>
      ))}
    </div>
  )
}

/** Large typographic section tabs («Мечты  Рецепты») with a duo-gradient indicator. */
export function SectionTabs<T extends string>({
  options,
  value,
  onChange,
  label,
}: {
  options: readonly { value: T; label: string }[]
  value: T
  onChange: (value: T) => void
  label: string
}) {
  const tg = useTelegram()
  const refs = useRef<(HTMLButtonElement | null)[]>([])
  const index = Math.max(0, options.findIndex((o) => o.value === value))

  const select = (i: number, fromKeyboard: boolean) => {
    const option = options[i]
    if (!option) return
    if (option.value !== value) {
      tg.haptic.selection()
      onChange(option.value)
    }
    if (fromKeyboard) refs.current[i]?.focus()
  }
  const onKeyDown = useRovingKeys(options.length, index, select)

  return (
    <div className="section-tabs" role="tablist" aria-label={label} onKeyDown={onKeyDown}>
      {options.map((o, i) => (
        <button
          key={o.value}
          ref={(el) => {
            refs.current[i] = el
          }}
          type="button"
          role="tab"
          aria-selected={i === index}
          tabIndex={i === index ? 0 : -1}
          className="section-tab"
          onClick={() => select(i, false)}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

const STEPS: readonly { value: Status; label: string; icon: ReactNode }[] = [
  { value: 'want', label: 'Хотим', icon: <IconHeart size={18} /> },
  { value: 'progress', label: 'Копим', icon: <IconPiggy size={18} /> },
  { value: 'done', label: 'Сбылось', icon: <IconSparkles size={18} /> },
]

const BURST = [0, 1, 2, 3, 4, 5] as const

/** «Путь мечты»: Хотим → Копим → Сбылось as a stepper with a duo-gradient track. */
export function StatusStepper({
  value,
  onChange,
  busy = false,
  label = 'Статус',
}: {
  value: Status
  onChange: (value: Status) => void
  busy?: boolean
  label?: string
}) {
  const tg = useTelegram()
  const refs = useRef<(HTMLButtonElement | null)[]>([])
  const index = Math.max(0, STEPS.findIndex((s) => s.value === value))

  // A sparkle burst each time the dream comes true (not on first render).
  const [burst, setBurst] = useState(0)
  const previous = useRef(value)
  useEffect(() => {
    if (value === 'done' && previous.current !== 'done') setBurst((n) => n + 1)
    previous.current = value
  }, [value])

  const select = (i: number, fromKeyboard: boolean) => {
    const step = STEPS[i]
    if (!step || busy) return
    if (step.value !== value) {
      tg.haptic.selection()
      onChange(step.value)
    }
    if (fromKeyboard) refs.current[i]?.focus()
  }
  const onKeyDown = useRovingKeys(STEPS.length, index, select)

  return (
    <div
      className="stepper"
      role="radiogroup"
      aria-label={label}
      aria-busy={busy}
      style={{ '--p': index / (STEPS.length - 1) } as CSSProperties}
      onKeyDown={onKeyDown}
    >
      <span className="stepper__track" aria-hidden="true">
        <span className="stepper__fill" />
      </span>
      {STEPS.map((s, i) => (
        <button
          key={s.value}
          ref={(el) => {
            refs.current[i] = el
          }}
          type="button"
          role="radio"
          aria-checked={i === index}
          tabIndex={i === index ? 0 : -1}
          className="step"
          data-state={i <= index ? 'reached' : 'todo'}
          onClick={() => select(i, false)}
        >
          <span className="step__dot">
            {s.icon}
            {s.value === 'done' && burst > 0 && (
              <span key={burst} className="burst" aria-hidden="true">
                {BURST.map((k) => (
                  <span key={k} style={{ '--k': k } as CSSProperties}>
                    <IconSparkles size={12} strokeWidth={2.2} />
                  </span>
                ))}
              </span>
            )}
          </span>
          <span className="step__label">{s.label}</span>
        </button>
      ))}
    </div>
  )
}

export function ChipGroup({ label, wrap, children }: { label: string; wrap?: boolean; children: ReactNode }) {
  return (
    <div className={cx('chips', wrap ? 'chips--wrap' : 'chips--scroll')} role="radiogroup" aria-label={label}>
      {children}
    </div>
  )
}

interface ChipProps {
  selected?: boolean
  count?: number
  emoji?: string
  onSelect: () => void
  /** An action chip (e.g. "+") is a plain button rather than a radio. */
  action?: boolean
  /** An icon-only action chip. */
  iconOnly?: boolean
  label?: string
  children: ReactNode
}

export function Chip({ selected = false, count, emoji, onSelect, action, iconOnly, label, children }: ChipProps) {
  const tg = useTelegram()
  const ref = useRef<HTMLButtonElement>(null)
  const mounted = useRef(false)

  // Bring a newly selected chip into view inside a horizontal scroller (not on first render).
  useEffect(() => {
    if (mounted.current && selected) ref.current?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'smooth' })
    mounted.current = true
  }, [selected])

  const onClick = () => {
    if (action) tg.haptic.impact('light')
    else if (!selected) tg.haptic.selection()
    onSelect()
  }
  return (
    <button
      ref={ref}
      type="button"
      className={cx('chip', selected && 'chip--selected', action && 'chip--action', iconOnly && 'chip--icon')}
      role={action ? undefined : 'radio'}
      aria-checked={action ? undefined : selected}
      aria-label={label}
      onClick={onClick}
    >
      {emoji && (
        <span className="chip__emoji" aria-hidden="true">
          {emoji}
        </span>
      )}
      <span className="chip__label">{children}</span>
      {count !== undefined && <span className="chip__count num">{count}</span>}
    </button>
  )
}

interface SwitchRowProps {
  label: ReactNode
  checked: boolean
  onChange: (checked: boolean) => void
  /** An icon tile before the label (iOS Settings style). */
  before?: ReactNode
  /** Revealed below the row while the switch is on. */
  children?: ReactNode
  haptic?: 'soft' | 'rigid'
}

export function SwitchRow({ label, checked, onChange, before, children, haptic = 'soft' }: SwitchRowProps) {
  const tg = useTelegram()
  const id = useId()
  return (
    <div className={cx('switch-row', before !== undefined && 'switch-row--icon')}>
      <label className="cell switch-row__cell" htmlFor={id}>
        {before !== undefined && <span className="cell__before">{before}</span>}
        <span className="cell__main">
          <span className="cell__title">{label}</span>
        </span>
        <input
          id={id}
          type="checkbox"
          role="switch"
          className="switch"
          checked={checked}
          onChange={(e) => {
            tg.haptic.impact(haptic)
            onChange(e.target.checked)
          }}
        />
      </label>
      {children !== undefined && (
        <div className="reveal" data-open={checked}>
          <div className="reveal__inner" inert={!checked}>
            {children}
          </div>
        </div>
      )}
    </div>
  )
}

export function SearchField({
  value,
  onChange,
  placeholder,
  label,
}: {
  value: string
  onChange: (value: string) => void
  placeholder: string
  label: string
}) {
  return (
    <div className="search">
      <span className="search__icon">
        <IconSearch size={18} />
      </span>
      <input
        type="search"
        className="search__input"
        aria-label={label}
        placeholder={placeholder}
        value={value}
        enterKeyHint="search"
        autoComplete="off"
        onChange={(e) => onChange(e.target.value)}
      />
      {value !== '' && (
        <button type="button" className="search__clear" aria-label="Очистить поиск" onClick={() => onChange('')}>
          <IconClear size={18} />
        </button>
      )}
    </div>
  )
}
