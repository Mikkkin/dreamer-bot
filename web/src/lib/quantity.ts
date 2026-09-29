// Ingredient and shopping-item amounts: the client twin of the server's
// domain.ParseQuantity, so forms send a canonical "1.5" + unit and can show
// a precise error before the request.

export const TO_TASTE = 'по вкусу'

const HUNDREDTHS = 100
const MAX_HUNDREDTHS = 100_000 * HUNDREDTHS
const MAX_INT_DIGITS = 6

export type AmountResult = { ok: true; amount: string } | { ok: false; message: string }

/** Parses "1,5", "1.5" or "200" into the canonical decimal the server renders ("1.5", "200", "0.25"). */
export function parseQuantityAmount(raw: string): AmountResult {
  const s = raw.replace(/\s/gu, '').replace(',', '.')
  const m = /^(\d+)(?:\.(\d*))?$/.exec(s)
  const frac = m?.[2] ?? ''
  if (!m?.[1] || frac.length > 2 || m[1].length > MAX_INT_DIGITS) {
    return { ok: false, message: 'Количество — число, например 1,5' }
  }
  const h = Number(m[1]) * HUNDREDTHS + Number(frac.padEnd(2, '0'))
  if (h <= 0) return { ok: false, message: 'Количество должно быть больше нуля' }
  if (h > MAX_HUNDREDTHS) return { ok: false, message: 'Слишком большое количество' }
  const units = Math.floor(h / HUNDREDTHS)
  const cents = h % HUNDREDTHS
  return { ok: true, amount: cents === 0 ? String(units) : `${units}.${String(cents).padStart(2, '0')}`.replace(/0$/, '') }
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

/** "1,5 кг", "3 шт", "по вкусу", "3" or "" — like the server's Quantity.Format (NBSP before the unit). */
export function formatQuantity(amount: string | null, unit: string | null): string {
  const a = amount ? amount.replace('.', ',') : ''
  if (a === '') return unit ?? ''
  return unit ? `${a}\u00a0${unit}` : a
}

/** An API amount in an input field: "1.5" → "1,5". */
export function amountForEdit(amount: string | null): string {
  return amount ? amount.replace('.', ',') : ''
}

// Everyday spellings of the units, mapped onto Me.units.
const UNIT_ALIASES: Readonly<Record<string, string>> = {
  гр: 'г',
  'гр.': 'г',
  'г.': 'г',
  грамм: 'г',
  'кг.': 'кг',
  'мл.': 'мл',
  'л.': 'л',
  'шт.': 'шт',
  штук: 'шт',
  штуки: 'шт',
  'ст.л.': 'ст. л.',
  'ст.л': 'ст. л.',
  'ч.л.': 'ч. л.',
  'ч.л': 'ч. л.',
  стакана: 'стакан',
  стаканов: 'стакан',
  'уп.': 'упаковка',
  уп: 'упаковка',
  упаковки: 'упаковка',
  зубчика: 'зубчик',
  зубчиков: 'зубчик',
  пучка: 'пучок',
}

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

export interface ParsedItem {
  name: string
  amount: string | null
  unit: string | null
}

/**
 * Splits a typed shopping line such as "Молоко 1,5 л", "Мука 500г" or
 * "Соль по вкусу" into a name and a quantity. Anything that does not end in
 * a valid amount (and optional known unit) stays part of the name.
 */
export function parseItemText(text: string, units: readonly string[]): ParsedItem {
  const s = text.replace(/\s+/gu, ' ').trim()
  const plain: ParsedItem = { name: s, amount: null, unit: null }
  if (units.includes(TO_TASTE)) {
    const toTaste = new RegExp(`^(.*\\S)\\s+${escape(TO_TASTE)}$`, 'iu').exec(s)
    if (toTaste?.[1]) return { name: toTaste[1], amount: null, unit: TO_TASTE }
  }
  const spellings = new Map<string, string>()
  for (const u of units) if (u !== TO_TASTE) spellings.set(u.toLocaleLowerCase('ru'), u)
  for (const [alias, u] of Object.entries(UNIT_ALIASES)) if (units.includes(u)) spellings.set(alias, u)
  const alternation = [...spellings.keys()]
    .sort((a, b) => b.length - a.length)
    .map(escape)
    .join('|')
  const re = new RegExp(`^(.*\\S)\\s+(\\d+(?:[.,]\\d+)?)(?:\\s*(${alternation}))?$`, 'iu')
  const m = re.exec(s)
  if (!m?.[1] || !m[2]) return plain
  const amount = parseQuantityAmount(m[2])
  if (!amount.ok) return plain
  const unit = m[3] ? (spellings.get(m[3].toLocaleLowerCase('ru')) ?? null) : null
  return { name: m[1], amount: amount.amount, unit }
}
