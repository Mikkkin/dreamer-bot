// Ingredient and shopping-item amounts in forms: the client twin of the
// server's domain.ParseQuantity, so forms send a canonical "1.5" + unit code
// and can show a precise error before the request. The words and fractions
// live in units.ts.

import { TO_TASTE, decimalOf, parseAmount, parseUnit } from './units'

export { TO_TASTE, amountForEdit, formatQuantity } from './units'

export type AmountResult = { ok: true; amount: string } | { ok: false; message: string }

/** Parses «1,5», «1/2», «1½», «пол» or «200» into the canonical decimal the server stores ("1.5", "0.5", "200"). */
export function parseQuantityAmount(raw: string): AmountResult {
  const r = parseAmount(raw)
  return r.ok ? { ok: true, amount: decimalOf(r.hundredths) } : r
}

export interface QuantityValue {
  amount: string | null
  unit: string | null
}

export type QuantityResult = { ok: true; value: QuantityValue } | { ok: false; message: string }

/**
 * Validates an amount and a unit picked from Me.units. «по вкусу» never has
 * an amount; any other unit needs one (the server rejects a bare unit too).
 */
export function checkQuantity(amountRaw: string, unitRaw: string, units: readonly string[]): QuantityResult {
  const unit = unitRaw.trim()
  const amount = amountRaw.trim()
  if (unit !== '' && !units.includes(unit)) return { ok: false, message: 'Неизвестная единица измерения' }
  if (unit === TO_TASTE) {
    return amount === '' ? { ok: true, value: { amount: null, unit } } : { ok: false, message: 'Для «по вкусу» количество не указывается' }
  }
  if (amount === '') return unit === '' ? { ok: true, value: { amount: null, unit: null } } : { ok: false, message: 'Укажите количество' }
  const r = parseQuantityAmount(amount)
  if (!r.ok) return r
  return { ok: true, value: { amount: r.amount, unit: unit === '' ? null : unit } }
}

export interface ParsedItem {
  name: string
  amount: string | null
  unit: string | null
}

const VULGAR = '½⅓⅔¼¾⅕⅖⅗⅘⅒'
// The last number of a line: «1,5», «1/2», «½», «1½» (a whole part before a
// separated fraction is joined below). Whatever follows must be a unit.
const ITEM_RE = new RegExp(`^(.*\\S)\\s+(\\d+(?:[.,]\\d+)?[${VULGAR}]?|\\d+\\/\\d+|[${VULGAR}])\\s*([^\\d${VULGAR}/]*)$`, 'u')
const TRAILING_WHOLE_RE = /^(.*\S)\s+(\d+)$/u

/**
 * Splits a typed shopping line such as «Молоко 1,5 л», «Мука 500г», «Сахар
 * 1 1/2 стакана» or «Соль по вкусу» into a name and a quantity. Anything that
 * does not end in a valid amount (and optional known unit) stays the name.
 */
export function parseItemText(text: string, units: readonly string[]): ParsedItem {
  const s = text.replace(/\s+/gu, ' ').trim()
  const plain: ParsedItem = { name: s, amount: null, unit: null }
  if (units.includes(TO_TASTE)) {
    const toTaste = new RegExp(`^(.*\\S)\\s+${TO_TASTE}$`, 'iu').exec(s)
    if (toTaste?.[1]) return { name: toTaste[1], amount: null, unit: TO_TASTE }
  }
  const m = ITEM_RE.exec(s)
  if (!m?.[1] || !m[2]) return plain
  let name = m[1]
  let amountRaw = m[2]
  // «Сахар 1 1/2» and «Сахар 1 ½»: the whole part ended up in the name.
  const whole = /^(\d+\/\d+|[½⅓⅔¼¾⅕⅖⅗⅘⅒])$/u.test(amountRaw) ? TRAILING_WHOLE_RE.exec(name) : null
  if (whole?.[1] && whole[2]) {
    const joined = parseAmount(`${whole[2]} ${amountRaw}`)
    if (joined.ok) {
      name = whole[1]
      amountRaw = `${whole[2]} ${amountRaw}`
    }
  }
  const amount = parseAmount(amountRaw)
  if (!amount.ok) return plain
  const unit = parseUnit(m[3] ?? '', units)
  if (unit === null || unit === TO_TASTE) return plain
  return { name, amount: decimalOf(amount.hundredths), unit: unit === '' ? null : unit }
}
