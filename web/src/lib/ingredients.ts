import type { Ingredient, IngredientInput } from '../api/types'
import { amountForEdit, checkQuantity } from './quantity'

// The recipe form's ingredient rows and their validation (the server's
// NormalizeIngredients, row by row, so each row can show its own error).

export interface IngredientRow {
  /** A stable React key; never sent. */
  key: number
  name: string
  amount: string
  unit: string
}

export function ingredientRows(list: readonly Ingredient[]): IngredientRow[] {
  return list.map((ing, i) => ({ key: i + 1, name: ing.name, amount: amountForEdit(ing.amount), unit: ing.unit ?? '' }))
}

export function emptyRow(rows: readonly IngredientRow[]): IngredientRow {
  return { key: rows.reduce((m, r) => Math.max(m, r.key), 0) + 1, name: '', amount: '', unit: '' }
}

export type RowErrors = Partial<Record<number, string>>

export type IngredientsCheck = { ok: true; ingredients: IngredientInput[] } | { ok: false; errors: RowErrors; message?: string }

const codePoints = (s: string) => [...s].length

/**
 * Validates the rows like the server. Blank rows (no name, no amount) are
 * skipped, so a trailing empty row never blocks saving.
 */
export function checkIngredients(
  rows: readonly IngredientRow[],
  units: readonly string[],
  limits: { nameMax: number; max: number },
): IngredientsCheck {
  const errors: RowErrors = {}
  const out: IngredientInput[] = []
  for (const row of rows) {
    const name = row.name.replace(/\s+/gu, ' ').trim()
    if (name === '' && row.amount.trim() === '') continue
    if (name === '') {
      errors[row.key] = 'Укажите название'
      continue
    }
    if (codePoints(name) > limits.nameMax) {
      errors[row.key] = `Название не длиннее ${limits.nameMax} символов`
      continue
    }
    const q = checkQuantity(row.amount, row.unit, units)
    if (!q.ok) {
      errors[row.key] = q.message
      continue
    }
    out.push({ name, amount: q.value.amount, unit: q.value.unit })
  }
  if (Object.keys(errors).length > 0) return { ok: false, errors }
  if (out.length > limits.max) return { ok: false, errors: {}, message: `Не больше ${limits.max} ингредиентов` }
  return { ok: true, ingredients: out }
}
