import type { SyntheticEvent } from 'react'
import { cx } from '../../lib/cx'
import { FRACTION_CHIPS, chipOf, joinFraction, unitOptionLabel, type UnitFormsTable } from '../../lib/units'
import { useTelegram } from '../../telegram/hooks'
import { IconChevronDown } from '../../ui/icons'

// Amount and unit inputs shared by the recipe form and the shopping item
// sheet: fraction chips that join the whole part, and a unit picker that
// speaks in words declined for the typed amount.

/**
 * Keeps the keyboard up: a chip tap must not blur the amount field it edits
 * (the chips under a recipe row disappear with the focus).
 */
const keepFocus = (e: SyntheticEvent) => e.preventDefault()

interface FractionChipsProps {
  /** What the amount field holds now. */
  value: string
  onChange: (value: string) => void
  /** Names the field for assistive tech, e.g. «Количество: мука». */
  label: string
  className?: string
}

/** [¼] [⅓] [½] [⅔] [¾]: «1» + ½ → «1½»; the chip already there removes itself. */
export function FractionChips({ value, onChange, label, className }: FractionChipsProps) {
  const tg = useTelegram()
  const current = chipOf(value)
  return (
    <div className={cx('frac-chips', className)} role="group" aria-label={`Дробь — ${label}`}>
      {FRACTION_CHIPS.map((chip) => (
        <button
          key={chip}
          type="button"
          className="frac-chip num"
          aria-pressed={chip === current}
          onPointerDown={keepFocus}
          onMouseDown={keepFocus}
          onClick={() => {
            tg.haptic.selection()
            onChange(joinFraction(value, chip))
          }}
        >
          {chip}
        </button>
      ))}
    </div>
  )
}

interface UnitSelectProps {
  value: string
  units: readonly string[]
  /** The typed amount: «2» shows «чайные ложки», «½» shows «чайной ложки». */
  amount: string
  forms?: UnitFormsTable | null
  label: string
  onChange: (unit: string) => void
  /** The taller variant that sits beside a text field. */
  field?: boolean
}

/** A native picker: the code as the value, the declined word as the label. */
export function UnitSelect({ value, units, amount, forms, label, onChange, field }: UnitSelectProps) {
  const tg = useTelegram()
  return (
    <span className={cx('unit-select', field && 'unit-select--field')}>
      <select
        aria-label={label}
        value={value}
        onChange={(e) => {
          tg.haptic.selection()
          onChange(e.target.value)
        }}
      >
        <option value="">ед.</option>
        {units.map((u) => (
          <option key={u} value={u}>
            {unitOptionLabel(u, amount, forms)}
          </option>
        ))}
      </select>
      <IconChevronDown size={14} strokeWidth={2.4} />
    </span>
  )
}
