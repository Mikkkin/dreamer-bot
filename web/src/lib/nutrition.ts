// КБЖУ on the client: the same tenths arithmetic as the server's
// domain.Nutrition, so the form's live preview shows exactly what the server
// will compute. The API always stores per-100 g values; whole-dish input is
// divided by weight / 100 before sending.

import type { Macros as ApiMacros, Nutrition, NutritionInput } from '../api/types'

export type MacroKey = 'kcal' | 'protein' | 'fat' | 'carbs'
export const MACRO_KEYS: readonly MacroKey[] = ['kcal', 'protein', 'fat', 'carbs']

/** One КБЖУ set in tenths (kcal and grams). */
export type Tenths = Record<MacroKey, number>

const TENTHS = 10
/** Pure fat is ~900 kcal per 100 g. */
export const KCAL_MAX_TENTHS = 900 * TENTHS
/** Grams of protein, fat or carbs per 100 g. */
export const MACRO_MAX_TENTHS = 100 * TENTHS
/** The server allows five integer digits per 100 g; a whole dish may be larger. */
const PER100_INT_DIGITS = 5
const DISH_INT_DIGITS = 7

/** short: the tile letter; abbr: fits a narrow tile; long: for labels and screen readers. */
export const MACRO_LABEL: Readonly<Record<MacroKey, { short: string; abbr: string; long: string; unit: string }>> = {
  kcal: { short: 'К', abbr: 'ккал', long: 'Калории', unit: 'ккал' },
  protein: { short: 'Б', abbr: 'белки', long: 'Белки', unit: 'г' },
  fat: { short: 'Ж', abbr: 'жиры', long: 'Жиры', unit: 'г' },
  carbs: { short: 'У', abbr: 'углев.', long: 'Углеводы', unit: 'г' },
}

/** Go's (a + b/2) / b on non-negative integers: round half up. */
export function roundDiv(a: number, b: number): number {
  return Math.floor((a + Math.floor(b / 2)) / b)
}

export type TenthsResult = { ok: true; tenths: number | null } | { ok: false; message: string }

/**
 * Parses "12", "12,5" or "12.5" into tenths; an empty field is null. Like the
 * server's ParseTenths it accepts at most one decimal place. Spaces are
 * ignored, so "1 200" works for whole-dish values.
 */
export function parseTenthsInput(raw: string, maxIntDigits = PER100_INT_DIGITS): TenthsResult {
  const s = raw.replace(/\s/gu, '').replace(',', '.')
  if (s === '') return { ok: true, tenths: null }
  const m = /^(\d+)(?:\.(\d*))?$/.exec(s)
  if (!m?.[1]) return { ok: false, message: 'Введите число, например 12,5' }
  const frac = m[2] ?? ''
  if (frac.length > 1) return { ok: false, message: 'Не больше одного знака после запятой' }
  if (m[1].length > maxIntDigits) return { ok: false, message: 'Слишком большое значение' }
  return { ok: true, tenths: Number(m[1]) * TENTHS + (frac === '' ? 0 : Number(frac)) }
}

/** Tenths as a Russian decimal: 125 → "12,5", 60 → "6". */
export function formatTenths(v: number): string {
  const whole = Math.floor(v / TENTHS)
  const f = v % TENTHS
  return f === 0 ? String(whole) : `${whole},${f}`
}

/** Tenths as a canonical machine decimal for the API: 125 → "12.5". */
export function decimalTenths(v: number): string {
  return formatTenths(v).replace(',', '.')
}

/** Parses an API decimal such as "12.5" into tenths; null for anything else. */
export function apiTenths(s: string): number | null {
  const m = /^(\d{1,7})(?:\.(\d))?$/.exec(s.trim())
  if (!m?.[1]) return null
  return Number(m[1]) * TENTHS + Number(m[2] ?? '0')
}

/** An API decimal for people: "12.5" → "12,5". */
export function formatApiDecimal(s: string): string {
  return s.replace('.', ',')
}

function mapTenths(t: Tenths, fn: (v: number) => number): Tenths {
  return { kcal: fn(t.kcal), protein: fn(t.protein), fat: fn(t.fat), carbs: fn(t.carbs) }
}

/** Per-100 g values scaled to the whole dish (server: PerDish). */
export function perDish(per100: Tenths, weightG: number): Tenths {
  return mapTenths(per100, (v) => roundDiv(v * weightG, 100))
}

/** One serving of the whole dish (server: PerServing). */
export function perServing(per100: Tenths, weightG: number, servings: number): Tenths {
  return mapTenths(perDish(per100, weightG), (v) => roundDiv(v, servings))
}

/** Whole-dish values brought back to per 100 g, rounded to 0.1. */
export function per100FromDish(dish: Tenths, weightG: number): Tenths {
  return mapTenths(dish, (v) => roundDiv(v * 100, weightG))
}

export function tenthsFromApi(m: ApiMacros): Tenths | null {
  const kcal = apiTenths(m.kcal)
  const protein = apiTenths(m.protein)
  const fat = apiTenths(m.fat)
  const carbs = apiTenths(m.carbs)
  if (kcal === null || protein === null || fat === null || carbs === null) return null
  return { kcal, protein, fat, carbs }
}

// ---------------------------------------------------------------- form --

export type NutritionMode = 'per100' | 'dish'

/** What the user typed in the recipe form. */
export interface NutritionDraft {
  mode: NutritionMode
  kcal: string
  protein: string
  fat: string
  carbs: string
  weight: string
  servings: string
}

export type NutritionField = MacroKey | 'weight_g' | 'servings'
export type NutritionErrors = Partial<Record<NutritionField, string>>

export interface NutritionLimits {
  dish_weight_max_g: number
  servings_max: number
}

export type NutritionCheck =
  | {
      ok: true
      /** What to send; null when nothing was entered. */
      input: NutritionInput | null
      per100: Tenths | null
      dish: Tenths | null
      serving: Tenths | null
    }
  | { ok: false; errors: NutritionErrors }

export const EMPTY_NUTRITION: NutritionDraft = { mode: 'per100', kcal: '', protein: '', fat: '', carbs: '', weight: '', servings: '' }

/** The form's starting values: the stored per-100 g numbers with a decimal comma. */
export function nutritionDraft(n: Nutrition | null): NutritionDraft {
  if (!n) return EMPTY_NUTRITION
  return {
    mode: 'per100',
    kcal: formatApiDecimal(n.per_100g.kcal),
    protein: formatApiDecimal(n.per_100g.protein),
    fat: formatApiDecimal(n.per_100g.fat),
    carbs: formatApiDecimal(n.per_100g.carbs),
    weight: n.weight_g ? String(n.weight_g) : '',
    servings: n.servings ? String(n.servings) : '',
  }
}

type IntResult = { ok: true; value: number | null } | { ok: false; message: string }

function parseCount(raw: string, max: number, range: string): IntResult {
  const s = raw.replace(/\s/gu, '')
  if (s === '') return { ok: true, value: null }
  if (!/^\d{1,6}$/.test(s)) return { ok: false, message: 'Целое число' }
  const n = Number(s)
  if (n < 1 || n > max) return { ok: false, message: range }
  return { ok: true, value: n }
}

function maxFor(key: MacroKey): number {
  return key === 'kcal' ? KCAL_MAX_TENTHS : MACRO_MAX_TENTHS
}

function rangeMessage(key: MacroKey, mode: NutritionMode): string {
  const base = key === 'kcal' ? 'До 900 ккал на 100 г' : 'До 100 г на 100 г'
  return mode === 'dish' ? `${base} — проверьте вес блюда` : base
}

/**
 * Validates the draft like the server does and computes every view of it.
 * In whole-dish mode the weight is required, and the values are converted to
 * per 100 g before the range check. All four values are required, unless
 * allowBlank is set (converting a half-typed draft between modes).
 */
export function checkNutrition(d: NutritionDraft, limits: NutritionLimits, { allowBlank = false } = {}): NutritionCheck {
  const errors: NutritionErrors = {}
  const weight = parseCount(d.weight, limits.dish_weight_max_g, `Вес блюда от 1 до ${limits.dish_weight_max_g} г`)
  if (!weight.ok) errors.weight_g = weight.message
  const servings = parseCount(d.servings, limits.servings_max, `Порций от 1 до ${limits.servings_max}`)
  if (!servings.ok) errors.servings = servings.message

  const typed: Partial<Tenths> = {}
  let any = false
  for (const key of MACRO_KEYS) {
    const r = parseTenthsInput(d[key], d.mode === 'dish' ? DISH_INT_DIGITS : PER100_INT_DIGITS)
    if (!r.ok) errors[key] = r.message
    else if (r.tenths !== null) {
      typed[key] = r.tenths
      any = true
    }
  }
  const weightG = weight.ok ? weight.value : null
  const servingsN = servings.ok ? servings.value : null
  if (!any && weightG === null && servingsN === null && Object.keys(errors).length === 0) {
    return { ok: true, input: null, per100: null, dish: null, serving: null }
  }
  if (d.mode === 'dish' && weightG === null && !errors.weight_g) errors.weight_g = 'Укажите вес блюда — по нему считаем на 100 г'
  if (Object.keys(errors).length > 0) return { ok: false, errors }

  const entered: Tenths = { kcal: typed.kcal ?? 0, protein: typed.protein ?? 0, fat: typed.fat ?? 0, carbs: typed.carbs ?? 0 }
  const per100 = d.mode === 'dish' && weightG !== null ? per100FromDish(entered, weightG) : entered
  for (const key of MACRO_KEYS) {
    if (per100[key] > maxFor(key)) errors[key] = rangeMessage(key, d.mode)
  }
  if (Object.keys(errors).length > 0) return { ok: false, errors }
  // Like the server: a blank value is unknown, not zero («0 ккал» would be made up).
  for (const key of MACRO_KEYS) {
    if (typed[key] === undefined && !allowBlank) errors[key] = 'Укажите — можно 0'
  }
  if (Object.keys(errors).length > 0) return { ok: false, errors }

  return {
    ok: true,
    input: {
      kcal: decimalTenths(per100.kcal),
      protein: decimalTenths(per100.protein),
      fat: decimalTenths(per100.fat),
      carbs: decimalTenths(per100.carbs),
      weight_g: weightG,
      servings: servingsN,
    },
    per100,
    dish: weightG !== null ? perDish(per100, weightG) : null,
    serving: weightG !== null && servingsN !== null ? perServing(per100, weightG, servingsN) : null,
  }
}

export type ModeSwitch = { ok: true; draft: NutritionDraft } | { ok: false; message: string }

/**
 * Switches the entry mode. Typed values are converted so the numbers on
 * screen keep their meaning. Without a valid weight they cannot be converted,
 * and silently reinterpreting "per 100 g" as "whole dish" would store wrong
 * КБЖУ, so the switch is refused until the weight is known.
 */
export function switchNutritionMode(d: NutritionDraft, mode: NutritionMode, limits: NutritionLimits): ModeSwitch {
  if (d.mode === mode) return { ok: true, draft: d }
  const typed = MACRO_KEYS.filter((key) => d[key].trim() !== '')
  if (typed.length === 0) return { ok: true, draft: { ...d, mode } }
  const check = checkNutrition(d, limits, { allowBlank: true })
  if (!check.ok) {
    return check.errors.weight_g || d.weight.trim() === ''
      ? { ok: false, message: 'Чтобы пересчитать, сначала укажите вес блюда' }
      : { ok: false, message: 'Сначала исправьте значения — потом пересчитаем' }
  }
  const values = mode === 'dish' ? check.dish : check.per100
  if (!values) return { ok: false, message: 'Чтобы пересчитать, сначала укажите вес блюда' }
  const out = { ...d, mode }
  for (const key of typed) out[key] = formatTenths(values[key])
  return { ok: true, draft: out }
}
