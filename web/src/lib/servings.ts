// The servings scaler. A recipe stores its amounts for `servings` portions;
// the detail screen shows them for another number of portions. Only the
// view changes: nothing here is ever sent back as the recipe.

import type { Ingredient, ShoppingItemInput } from '../api/types'
import { HUNDREDTHS, TO_TASTE, amountHundredths, decimalOf, formatHundredths, isFractionFriendly, type UnitFormsTable } from './units'

export const SERVINGS_MIN = 1

/** How many times the chosen portions are the recipe's: 6 of 4 → 1.5. */
export function scaleFactor(base: number, chosen: number): number {
  return base > 0 && chosen > 0 ? chosen / base : 1
}

/** The factor for a badge: «×1,5», «×2», «×0,33». */
export function formatFactor(factor: number): string {
  const rounded = Math.round(factor * 100) / 100
  return `×${String(rounded).replace('.', ',')}`
}

export type ServingsCheck = { ok: true; value: number | null } | { ok: false; message: string }

/** The recipe's servings as typed in the form: empty is unknown, otherwise a whole number in 1..max. */
export function checkServings(raw: string, max: number): ServingsCheck {
  const s = raw.replace(/\s/gu, '')
  if (s === '') return { ok: true, value: null }
  const n = /^\d{1,3}$/.test(s) ? Number(s) : Number.NaN
  if (!Number.isInteger(n) || n < SERVINGS_MIN || n > max) return { ok: false, message: `Порций — целое число от 1 до ${max}` }
  return { ok: true, value: n }
}

/** Steps the stepper within 1..max. */
export function stepServings(current: number, delta: number, max: number): number {
  return Math.min(max, Math.max(SERVINGS_MIN, Math.round(current) + delta))
}

/**
 * The value a stored amount stands for. Thirds are stored rounded (0.33,
 * 0.67) but mean exactly ⅓ and ⅔, so ⅓ стакана × 3 is 1 стакан, never 0,99.
 */
export function exactAmount(hundredths: number): number {
  const whole = Math.floor(hundredths / HUNDREDTHS)
  const frac = hundredths % HUNDREDTHS
  if (frac === 33) return whole + 1 / 3
  if (frac === 67) return whole + 2 / 3
  return hundredths / HUNDREDTHS
}

const EPS = 1e-9
const SNAP_WINDOW = 0.05
const SNAPS: readonly { value: number; hundredths: number }[] = [
  { value: 0, hundredths: 0 },
  { value: 1 / 4, hundredths: 25 },
  { value: 1 / 3, hundredths: 33 },
  { value: 1 / 2, hundredths: 50 },
  { value: 2 / 3, hundredths: 67 },
  { value: 3 / 4, hundredths: 75 },
  { value: 1, hundredths: 100 },
]

/**
 * Spoons, cups, pieces, кг, л and bare numbers: the nearest of whole, ¼, ⅓,
 * ½, ⅔, ¾ when within 0.05 of it, else one decimal (two for a tiny amount,
 * which must never become 0).
 */
export function snapCount(value: number): number {
  const whole = Math.floor(value + EPS)
  let best: number | null = null
  let bestDistance = Infinity
  for (const s of SNAPS) {
    const candidate = whole + s.value
    const distance = Math.abs(value - candidate)
    if (candidate > EPS && distance <= SNAP_WINDOW + EPS && distance < bestDistance) {
      best = whole * HUNDREDTHS + s.hundredths
      bestDistance = distance
    }
  }
  if (best !== null) return best
  const tenths = Math.round(value * 10 + EPS)
  if (tenths > 0) return tenths * 10
  return Math.max(1, Math.round(value * HUNDREDTHS))
}

export interface ScaledQuantity {
  hundredths: number
  unit: string | null
  /** A metric amount converted to кг / л: shown with a decimal comma («1,2 кг»). */
  decimal: boolean
}

const BIGGER: Readonly<Record<string, string>> = { г: 'кг', мл: 'л' }

/**
 * Grams and millilitres round to what a scale shows: under 10 whole, 10–100
 * in steps of 5, 100–1000 in steps of 10; from 1000 they become кг / л with
 * one decimal («1,2 кг», «1,5 л»).
 */
function roundMetric(value: number, unit: string): ScaledQuantity {
  let v: number
  if (value < 10) v = Math.round(value)
  else if (value < 100) v = Math.round(value / 5) * 5
  else if (value < 1000) v = Math.round(value / 10) * 10
  else v = value
  if (v >= 1000) {
    return { hundredths: Math.round(value / 100) * 10, unit: BIGGER[unit] ?? unit, decimal: true }
  }
  if (v === 0) return { hundredths: Math.max(10, Math.round(value * 10) * 10), unit, decimal: false }
  return { hundredths: v * HUNDREDTHS, unit, decimal: false }
}

/**
 * Scales one stored amount. «по вкусу» and lines without an amount never
 * change; a bare number scales like a count (2 → 3). Returns null when the
 * line stays as it is (no amount, or the factor is 1).
 */
export function scaleQuantity(hundredths: number | null, unit: string | null, factor: number): ScaledQuantity | null {
  if (hundredths === null || hundredths <= 0 || unit === TO_TASTE || Math.abs(factor - 1) < EPS) return null
  const value = exactAmount(hundredths) * factor
  if (unit !== null && !isFractionFriendly(unit)) return roundMetric(value, unit)
  return { hundredths: snapCount(value), unit, decimal: false }
}

export interface ScaledIngredient {
  name: string
  /** What to show: the amount and its declined unit. */
  text: string
  /** The amount for the shopping list; null without an amount. */
  amount: string | null
  unit: string | null
  /** The amount differs from the recipe as stored. */
  scaled: boolean
}

/** One ingredient line for the chosen portions, with the unit declined for the new amount. */
export function scaleIngredient(ing: Ingredient, factor: number, table?: UnitFormsTable | null): ScaledIngredient {
  const h = amountHundredths(ing.amount)
  const q = scaleQuantity(h, ing.unit, factor)
  if (!q) {
    return { name: ing.name, text: formatHundredths(h, ing.unit, table), amount: h === null ? null : decimalOf(h), unit: ing.unit, scaled: false }
  }
  return {
    name: ing.name,
    text: formatHundredths(q.hundredths, q.unit, table, { decimal: q.decimal }),
    amount: decimalOf(q.hundredths),
    unit: q.unit,
    scaled: q.hundredths !== h || q.unit !== ing.unit,
  }
}

/** The picked lines as POST /api/shopping items, with the scaled amounts. */
export function scaledShoppingItems(
  ingredients: readonly Ingredient[],
  positions: Iterable<number>,
  factor: number,
): ShoppingItemInput[] {
  const out: ShoppingItemInput[] = []
  for (const i of [...positions].sort((a, b) => a - b)) {
    const ing = ingredients[i]
    if (!ing) continue
    const s = scaleIngredient(ing, factor)
    out.push({ name: s.name, amount: s.amount, unit: s.unit })
  }
  return out
}
