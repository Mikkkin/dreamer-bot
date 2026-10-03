import { useId, useRef, useState } from 'react'
import { cx } from '../../lib/cx'
import {
  MACRO_KEYS,
  MACRO_LABEL,
  checkNutrition,
  formatTenths,
  switchNutritionMode,
  type MacroKey,
  type NutritionDraft,
  type NutritionErrors,
  type NutritionLimits,
  type NutritionMode,
  type Tenths,
} from '../../lib/nutrition'
import { useTelegram } from '../../telegram/hooks'
import { Segmented } from '../../ui/controls'

const MODES = [
  { value: 'per100', label: 'на 100 г' },
  { value: 'dish', label: 'на всё блюдо' },
] as const

function line(t: Tenths): string {
  return `${formatTenths(t.kcal)} ккал · Б ${formatTenths(t.protein)} · Ж ${formatTenths(t.fat)} · У ${formatTenths(t.carbs)}`
}

interface NutritionEditorProps {
  draft: NutritionDraft
  limits: NutritionLimits
  /** The recipe's servings (a field of the form), for the per-portion preview. */
  servings: number | null
  errors: NutritionErrors
  /** A message about КБЖУ as a whole (e.g. from the server). */
  error?: string
  onChange: (draft: NutritionDraft) => void
}

/**
 * КБЖУ by hand: per 100 g or for the whole dish, plus the dish weight. The
 * other values (and one portion, when the recipe's servings are known) are
 * previewed live with the server's rounding.
 */
export function NutritionEditor({ draft, limits, servings, errors, error, onChange }: NutritionEditorProps) {
  const id = useId()
  const tg = useTelegram()
  const weightInput = useRef<HTMLInputElement>(null)
  const [modeHint, setModeHint] = useState<string | null>(null)
  const check = checkNutrition(draft, limits, { servings })
  const set = (patch: Partial<NutritionDraft>) => {
    setModeHint(null)
    onChange({ ...draft, ...patch })
  }
  const setMode = (mode: NutritionMode) => {
    const r = switchNutritionMode(draft, mode, limits)
    if (r.ok) {
      setModeHint(null)
      onChange(r.draft)
      return
    }
    tg.haptic.notify('warning')
    setModeHint(r.message)
    weightInput.current?.focus()
  }

  const preview: string[] = []
  if (check.ok && check.per100) {
    if (draft.mode === 'dish') preview.push(`На 100 г: ${line(check.per100)}`)
    else if (check.dish) preview.push(`Всё блюдо: ${line(check.dish)}`)
    if (check.serving) preview.push(`Порция: ${line(check.serving)}`)
  }

  const macroField = (key: MacroKey) => {
    const fieldId = `${id}-${key}`
    const err = errors[key]
    return (
      <div key={key} className={cx('macro-input', err && 'macro-input--error')}>
        <label htmlFor={fieldId} className="macro-input__label">
          <span className="macro-input__letter" aria-hidden="true">
            {MACRO_LABEL[key].short}
          </span>
          {MACRO_LABEL[key].long}
        </label>
        <span className="macro-input__box">
          <input
            id={fieldId}
            className="macro-input__value num"
            inputMode="decimal"
            placeholder="0"
            autoComplete="off"
            enterKeyHint="next"
            value={draft[key]}
            aria-invalid={err ? true : undefined}
            aria-describedby={err ? `${fieldId}-error` : undefined}
            onChange={(e) => set({ [key]: e.target.value })}
          />
          <span className="macro-input__unit" aria-hidden="true">
            {MACRO_LABEL[key].unit}
          </span>
        </span>
        {err && (
          <span id={`${fieldId}-error`} className="macro-input__error">
            {err}
          </span>
        )}
      </div>
    )
  }

  const weightField = (label: string) => {
    const fieldId = `${id}-weight`
    const err = errors.weight_g
    return (
      <div className={cx('macro-input', 'macro-input--wide', err && 'macro-input--error')}>
        <label htmlFor={fieldId} className="macro-input__label">
          {label}
        </label>
        <span className="macro-input__box">
          <input
            ref={weightInput}
            id={fieldId}
            className="macro-input__value num"
            inputMode="numeric"
            placeholder="—"
            autoComplete="off"
            enterKeyHint="done"
            value={draft.weight}
            aria-invalid={err ? true : undefined}
            aria-describedby={err ? `${fieldId}-error` : undefined}
            onChange={(e) => set({ weight: e.target.value })}
          />
          <span className="macro-input__unit" aria-hidden="true">
            г
          </span>
        </span>
        {err && (
          <span id={`${fieldId}-error`} className="macro-input__error">
            {err}
          </span>
        )}
      </div>
    )
  }

  return (
    <div className="nutrition-editor">
      <Segmented label="Как указать КБЖУ" value={draft.mode} onChange={setMode} options={MODES} />
      {modeHint && (
        <p className="nutrition-editor__hint" role="alert">
          {modeHint}
        </p>
      )}
      <div className="macro-grid">{MACRO_KEYS.map(macroField)}</div>
      <div className="macro-grid macro-grid--two">{weightField(draft.mode === 'dish' ? 'Вес блюда (обязательно)' : 'Вес блюда')}</div>
      {preview.length > 0 && (
        <div className="nutrition-preview" aria-live="polite">
          {preview.map((p) => (
            <p key={p} className="num">
              {p}
            </p>
          ))}
        </div>
      )}
      {error && (
        <p className="ing-editor__error" role="alert">
          {error}
        </p>
      )}
    </div>
  )
}
