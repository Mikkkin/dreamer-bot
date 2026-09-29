import { useEffect, useRef, type KeyboardEvent } from 'react'
import { cx } from '../../lib/cx'
import { emptyRow, type IngredientRow, type RowErrors } from '../../lib/ingredients'
import { TO_TASTE } from '../../lib/quantity'
import { useTelegram } from '../../telegram/hooks'
import { IconChevronDown, IconPlus, IconX } from '../../ui/icons'

interface IngredientsEditorProps {
  rows: IngredientRow[]
  units: readonly string[]
  max: number
  nameMax: number
  errors: RowErrors
  /** A message about the list as a whole (e.g. from the server). */
  error?: string
  onChange: (rows: IngredientRow[]) => void
}

/** Structured ingredients: a name, an amount and a unit per row. */
export function IngredientsEditor({ rows, units, max, nameMax, errors, error, onChange }: IngredientsEditorProps) {
  const tg = useTelegram()
  const names = useRef(new Map<number, HTMLInputElement>())
  const amounts = useRef(new Map<number, HTMLInputElement>())
  const focusKey = useRef<number | null>(null)
  const lastKey = rows.at(-1)?.key

  // A freshly added row gets the keyboard.
  useEffect(() => {
    if (focusKey.current === null) return
    names.current.get(focusKey.current)?.focus()
    focusKey.current = null
  }, [lastKey])

  const update = (key: number, patch: Partial<IngredientRow>) => onChange(rows.map((r) => (r.key === key ? { ...r, ...patch } : r)))

  const add = () => {
    if (rows.length >= max) {
      tg.haptic.notify('warning')
      return
    }
    tg.haptic.impact('light')
    const row = emptyRow(rows)
    focusKey.current = row.key
    onChange([...rows, row])
  }

  const onAmountEnter = (e: KeyboardEvent<HTMLInputElement>, index: number) => {
    if (e.key !== 'Enter') return
    e.preventDefault()
    const next = rows[index + 1]
    if (next) names.current.get(next.key)?.focus()
    else add()
  }

  const labelOf = (row: IngredientRow, i: number) => row.name.trim() || `ингредиент ${i + 1}`

  return (
    <div className="ing-editor">
      {rows.length > 0 && (
        <ol className="ing-editor__list">
          {rows.map((row, i) => {
            const toTaste = row.unit === TO_TASTE
            const err = errors[row.key]
            return (
              <li key={row.key} className={cx('ing-row', err && 'ing-row--error')}>
                <input
                  ref={(el) => {
                    if (el) names.current.set(row.key, el)
                    else names.current.delete(row.key)
                  }}
                  className="ing-row__name"
                  aria-label={`Ингредиент ${i + 1}`}
                  aria-invalid={err ? true : undefined}
                  placeholder={i === 0 ? 'Например, спагетти' : 'Продукт'}
                  value={row.name}
                  maxLength={nameMax * 2}
                  autoComplete="off"
                  enterKeyHint="next"
                  onChange={(e) => update(row.key, { name: e.target.value })}
                  onKeyDown={(e) => {
                    if (e.key !== 'Enter') return
                    e.preventDefault()
                    amounts.current.get(row.key)?.focus()
                  }}
                />
                <div className="ing-row__qty">
                  <input
                    ref={(el) => {
                      if (el) amounts.current.set(row.key, el)
                      else amounts.current.delete(row.key)
                    }}
                    className="ing-row__amount num"
                    aria-label={`Количество: ${labelOf(row, i)}`}
                    placeholder={toTaste ? '—' : 'Кол-во'}
                    inputMode="decimal"
                    value={toTaste ? '' : row.amount}
                    disabled={toTaste}
                    autoComplete="off"
                    enterKeyHint={i === rows.length - 1 ? 'done' : 'next'}
                    onChange={(e) => update(row.key, { amount: e.target.value })}
                    onKeyDown={(e) => onAmountEnter(e, i)}
                  />
                  <span className="unit-select">
                    <select
                      aria-label={`Единица: ${labelOf(row, i)}`}
                      value={row.unit}
                      onChange={(e) => {
                        tg.haptic.selection()
                        const unit = e.target.value
                        update(row.key, unit === TO_TASTE ? { unit, amount: '' } : { unit })
                      }}
                    >
                      <option value="">ед.</option>
                      {units.map((u) => (
                        <option key={u} value={u}>
                          {u}
                        </option>
                      ))}
                    </select>
                    <IconChevronDown size={14} strokeWidth={2.4} />
                  </span>
                  <button
                    type="button"
                    className="ing-row__remove"
                    aria-label={`Убрать «${labelOf(row, i)}»`}
                    onClick={() => {
                      tg.haptic.impact('light')
                      onChange(rows.filter((r) => r.key !== row.key))
                    }}
                  >
                    <IconX size={16} strokeWidth={2.4} />
                  </button>
                </div>
                {err && <p className="ing-row__error">{err}</p>}
              </li>
            )
          })}
        </ol>
      )}
      {error && (
        <p className="ing-editor__error" role="alert">
          {error}
        </p>
      )}
      {rows.length < max && (
        <button type="button" className="add-row" onClick={add}>
          <IconPlus size={18} strokeWidth={2.4} />
          Ингредиент
        </button>
      )}
    </div>
  )
}
