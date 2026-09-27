import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  type FocusEvent,
  type InputHTMLAttributes,
  type ReactNode,
  type RefObject,
  type TextareaHTMLAttributes,
} from 'react'
import { cx } from '../lib/cx'

/** Characters as the server counts them (code points, not UTF-16 units). */
export function charCount(s: string): number {
  let n = 0
  for (const _ of s) n++
  return n
}

const SHAKE: Keyframe[] = [
  { transform: 'translateX(0)' },
  { transform: 'translateX(-4px)' },
  { transform: 'translateX(4px)' },
  { transform: 'translateX(-4px)' },
  { transform: 'translateX(0)' },
]

interface FieldChrome {
  label: string
  error?: string
  hint?: ReactNode
  /** Shows "n / max" once the value is longer than counterFrom. */
  max?: number
  counterFrom?: number
  /** Changing this number replays the error shake (one per failed submit). */
  shakeKey?: number
  value: string
  /** The label stays for assistive tech; a section header names the field instead. */
  hideLabel?: boolean
}

function useFieldChrome({ error, shakeKey }: Pick<FieldChrome, 'error' | 'shakeKey'>) {
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!error || !shakeKey || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    box.current?.animate(SHAKE, { duration: 240, easing: 'ease-in-out' })
  }, [shakeKey, error])
  return box
}

/** On iOS the keyboard covers focused inputs near the bottom; recenter once it is up. */
function scrollIntoViewSoon(e: FocusEvent<HTMLElement>) {
  const el = e.currentTarget
  window.setTimeout(() => {
    if (document.activeElement === el) el.scrollIntoView({ block: 'center', behavior: 'smooth' })
  }, 300)
}

function FieldFrame({
  id,
  chrome,
  box,
  children,
}: {
  id: string
  chrome: FieldChrome
  box: RefObject<HTMLDivElement | null>
  children: ReactNode
}) {
  const { label, error, hint, max, counterFrom, value, hideLabel } = chrome
  const count = max !== undefined ? charCount(value) : 0
  const showCounter = max !== undefined && count > (counterFrom ?? max * 0.8)
  return (
    <div className={cx('field', error && 'field--error')}>
      <div className="field__box" ref={box}>
        <div className="field__top">
          <label htmlFor={id} className={hideLabel ? 'visually-hidden' : 'field__label'}>
            {label}
          </label>
          {showCounter && (
            <span className={cx('field__counter num', count > max && 'field__counter--over')} aria-live="polite">
              {count} / {max}
            </span>
          )}
        </div>
        {children}
      </div>
      {error ? (
        <p id={`${id}-error`} className="field__error">
          {error}
        </p>
      ) : (
        hint && <p className="field__hint">{hint}</p>
      )}
    </div>
  )
}

type InputProps = FieldChrome & Omit<InputHTMLAttributes<HTMLInputElement>, 'id' | 'value' | 'max'>

export function TextField({
  label,
  error,
  hint,
  max,
  counterFrom,
  shakeKey,
  value,
  hideLabel,
  onFocus,
  className,
  ...input
}: InputProps) {
  const id = useId()
  const box = useFieldChrome({ error, shakeKey })
  return (
    <FieldFrame id={id} box={box} chrome={{ label, error, hint, max, counterFrom, value, hideLabel }}>
      <input
        id={id}
        className={cx('field__input', className)}
        value={value}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${id}-error` : undefined}
        onFocus={(e) => {
          scrollIntoViewSoon(e)
          onFocus?.(e)
        }}
        {...input}
      />
    </FieldFrame>
  )
}

type AreaProps = FieldChrome & Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, 'id' | 'value'> & { minRows?: number }

/** A textarea that grows with its content. */
export function TextArea({
  label,
  error,
  hint,
  max,
  counterFrom,
  shakeKey,
  value,
  hideLabel,
  minRows = 3,
  onFocus,
  className,
  ...area
}: AreaProps) {
  const id = useId()
  const box = useFieldChrome({ error, shakeKey })
  const ref = useRef<HTMLTextAreaElement>(null)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
  }, [value])

  return (
    <FieldFrame id={id} box={box} chrome={{ label, error, hint, max, counterFrom, value, hideLabel }}>
      <textarea
        ref={ref}
        id={id}
        className={cx('field__input field__input--area', className)}
        rows={minRows}
        value={value}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${id}-error` : undefined}
        onFocus={(e) => {
          scrollIntoViewSoon(e)
          onFocus?.(e)
        }}
        {...area}
      />
    </FieldFrame>
  )
}

interface AmountFieldProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'id' | 'value'> {
  label: string
  value: string
  error?: string
  shakeKey?: number
  /** Beside the number, e.g. the currency switcher. */
  trailing?: ReactNode
}

/** Money first, as in Wallet: a large number with the currency beside it. */
export function AmountField({ label, value, error, shakeKey, trailing, onFocus, className, ...input }: AmountFieldProps) {
  const id = useId()
  const box = useFieldChrome({ error, shakeKey })
  return (
    <div className={cx('amount', error && 'amount--error')}>
      <div className="amount__box" ref={box}>
        <label htmlFor={id} className="visually-hidden">
          {label}
        </label>
        <input
          id={id}
          className={cx('amount__input num', className)}
          value={value}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? `${id}-error` : undefined}
          onFocus={(e) => {
            scrollIntoViewSoon(e)
            onFocus?.(e)
          }}
          {...input}
        />
        {trailing}
      </div>
      {error && (
        <p id={`${id}-error`} className="field__error">
          {error}
        </p>
      )}
    </div>
  )
}
