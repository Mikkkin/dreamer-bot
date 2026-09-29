import { useEffect, useId, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from 'react'
import type { Status } from '../api/types'
import { cx } from '../lib/cx'
import { useTelegram } from '../telegram/hooks'
import { IconCheck, IconClear, IconHeart, IconPiggy, IconSearch, IconSparkles, IconStar } from './icons'

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

export function ChipGroup({ label, wrap, multi, children }: { label: string; wrap?: boolean; multi?: boolean; children: ReactNode }) {
  return (
    <div className={cx('chips', wrap ? 'chips--wrap' : 'chips--scroll')} role={multi ? 'group' : 'radiogroup'} aria-label={label}>
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
  /** A checkbox in a multi-select group rather than a radio. */
  multi?: boolean
  label?: string
  children: ReactNode
}

export function Chip({ selected = false, count, emoji, onSelect, action, iconOnly, multi, label, children }: ChipProps) {
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
    else if (multi || !selected) tg.haptic.selection()
    onSelect()
  }
  return (
    <button
      ref={ref}
      type="button"
      className={cx('chip', selected && 'chip--selected', action && 'chip--action', iconOnly && 'chip--icon')}
      role={action ? undefined : multi ? 'checkbox' : 'radio'}
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

/**
 * A compact single choice with content-sized options (sorting): the capsule
 * of the segmented control, but each option only as wide as its label.
 */
export function ChoicePills<T extends string>({
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
    <div className="pills" role="radiogroup" aria-label={label} onKeyDown={onKeyDown}>
      {options.map((o, i) => (
        <button
          key={o.value}
          ref={(el) => {
            refs.current[i] = el
          }}
          type="button"
          role="radio"
          aria-checked={i === index}
          tabIndex={i === index ? 0 : -1}
          className="pill"
          onClick={() => select(i, false)}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

/** A round checkbox, as in Reminders: a light tap when ticked, a selection tick when cleared. */
export function RoundCheck({ checked, onChange, label }: { checked: boolean; onChange: (checked: boolean) => void; label: string }) {
  const tg = useTelegram()
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      aria-label={label}
      className="round-check"
      onClick={() => {
        if (checked) tg.haptic.selection()
        else tg.haptic.impact('light')
        onChange(!checked)
      }}
    >
      <span className="round-check__box" aria-hidden="true">
        {checked && <IconCheck size={14} strokeWidth={3.2} />}
      </span>
    </button>
  )
}

const STAR_VALUES = [1, 2, 3, 4, 5] as const

/** Five big stars; each tap ticks the selection haptic and pops the chosen star. */
export function StarRating({ value, onChange, label = 'Оценка' }: { value: number; onChange: (stars: number) => void; label?: string }) {
  const tg = useTelegram()
  const refs = useRef<(HTMLButtonElement | null)[]>([])
  const [popped, setPopped] = useState(0)

  const select = (i: number, fromKeyboard: boolean) => {
    const stars = STAR_VALUES[i]
    if (stars === undefined) return
    tg.haptic.selection()
    setPopped((n) => n + 1)
    onChange(stars)
    if (fromKeyboard) refs.current[i]?.focus()
  }
  const current = Math.max(0, value - 1)
  const onKeyDown = useRovingKeys(STAR_VALUES.length, current, select)

  return (
    <div className="stars-input" role="radiogroup" aria-label={label} onKeyDown={onKeyDown}>
      {STAR_VALUES.map((n, i) => (
        <button
          key={n}
          ref={(el) => {
            refs.current[i] = el
          }}
          type="button"
          role="radio"
          aria-checked={n === value}
          aria-label={`${n} из 5`}
          tabIndex={n === value || (value === 0 && n === 1) ? 0 : -1}
          className={cx('star-btn', n <= value && 'star-btn--on')}
          onClick={() => select(i, false)}
        >
          <span key={n === value ? popped : 0} className={cx('star-btn__icon', n === value && popped > 0 && 'star-btn__icon--pop')}>
            <IconStar size={40} strokeWidth={1.6} className={n <= value ? 'icon-fill' : undefined} />
          </span>
        </button>
      ))}
    </div>
  )
}

/** A slim duo-gradient progress bar; value is 0..1. */
export function ProgressBar({ value, label, size = 'slim' }: { value: number; label: string; size?: 'slim' | 'big' }) {
  const v = Math.min(1, Math.max(0, value))
  return (
    <span
      className={cx('progress', `progress--${size}`, v >= 1 && 'progress--full')}
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(v * 100)}
    >
      <span className="progress__fill" style={{ '--p': v } as CSSProperties} />
    </span>
  )
}
