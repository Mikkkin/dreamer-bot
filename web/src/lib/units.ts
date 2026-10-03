// Kitchen units and amounts as people read and type them: the client twin of
// the server's domain units (Unit.Forms, ParseQuantityAmount, ParseUnit,
// Quantity.Format). Both sides are checked against the shared vectors in
// internal/domain/testdata. Amounts are integer hundredths, as stored.

export const TO_TASTE = 'по вкусу'
export const NBSP = ' '

export const HUNDREDTHS = 100
/** 100 000 of any unit, like the server's MaxQuantityHundredths. */
export const MAX_HUNDREDTHS = 100_000 * HUNDREDTHS

export type PluralForm = 'one' | 'few' | 'many' | 'fraction'

/** The words of a unit after an amount: «1 чайная ложка», «2 чайные ложки», «5 чайных ложек», «½ чайной ложки». */
export interface UnitForms {
  one: string
  few: string
  many: string
  fraction: string
}

export type UnitFormsTable = Readonly<Record<string, UnitForms>>

/**
 * The units whose words change with the amount, exactly as the server's
 * domain table. Me.unit_forms is the source of truth; this table covers an
 * older server or a code it does not list. Every other unit is invariant.
 */
export const DECLINED_UNITS: UnitFormsTable = {
  'ст. л.': { one: 'столовая ложка', few: 'столовые ложки', many: 'столовых ложек', fraction: 'столовой ложки' },
  'ч. л.': { one: 'чайная ложка', few: 'чайные ложки', many: 'чайных ложек', fraction: 'чайной ложки' },
  стакан: { one: 'стакан', few: 'стакана', many: 'стаканов', fraction: 'стакана' },
  щепотка: { one: 'щепотка', few: 'щепотки', many: 'щепоток', fraction: 'щепотки' },
  зубчик: { one: 'зубчик', few: 'зубчика', many: 'зубчиков', fraction: 'зубчика' },
  пучок: { one: 'пучок', few: 'пучка', many: 'пучков', fraction: 'пучка' },
  упаковка: { one: 'упаковка', few: 'упаковки', many: 'упаковок', fraction: 'упаковки' },
}

/** Every unit code in picker order, as the server's domain.Units. */
export const UNIT_CODES: readonly string[] = [
  'г',
  'кг',
  'мл',
  'л',
  'шт',
  'ст. л.',
  'ч. л.',
  'стакан',
  'щепотка',
  'зубчик',
  'пучок',
  'упаковка',
  TO_TASTE,
]

/**
 * The grammatical number after an amount in hundredths. A whole n takes
 * «one» when it ends in 1 but not in 11 (1, 21, 101), «few» when it ends in
 * 2–4 but not in 12–14 (2, 23, 104), «many» otherwise; anything that is not
 * whole takes «fraction» (the genitive singular). Deliberately not
 * Intl.PluralRules: the result must match the server on every engine.
 */
export function pluralFor(hundredths: number): PluralForm {
  const h = Math.abs(Math.round(hundredths))
  if (h % HUNDREDTHS !== 0) return 'fraction'
  const n = h / HUNDREDTHS
  const last = n % 10
  const lastTwo = n % 100
  if (last === 1 && lastTwo !== 11) return 'one'
  if (last >= 2 && last <= 4 && (lastTwo < 12 || lastTwo > 14)) return 'few'
  return 'many'
}

function isForms(v: unknown): v is UnitForms {
  if (typeof v !== 'object' || v === null) return false
  const f = v as Record<string, unknown>
  return typeof f.one === 'string' && typeof f.few === 'string' && typeof f.many === 'string' && typeof f.fraction === 'string'
}

/** The words of a unit: from Me.unit_forms when it lists the code, else the built-in table; invariant units repeat the code. */
export function unitForms(unit: string, table?: UnitFormsTable | null): UnitForms {
  const served: unknown = table?.[unit]
  if (isForms(served) && served.one !== '') return served
  return DECLINED_UNITS[unit] ?? { one: unit, few: unit, many: unit, fraction: unit }
}

/**
 * The unit's word for an amount («чайные ложки» for 2 ч. л.), or null without
 * a unit. Without an amount it is the dictionary form, as on the server.
 */
export function unitLabel(unit: string | null, hundredths: number | null, table?: UnitFormsTable | null): string | null {
  if (!unit) return null
  const forms = unitForms(unit, table)
  return hundredths ? forms[pluralFor(hundredths)] : forms.one
}

/** Grams and millilitres are precise measures and keep a decimal comma; everything else shows ½ ⅓ ¼ … */
export function isFractionFriendly(unit: string | null): boolean {
  return unit !== 'г' && unit !== 'мл'
}

const VULGAR_BY_HUNDREDTHS: Readonly<Record<number, string>> = { 25: '¼', 33: '⅓', 50: '½', 67: '⅔', 75: '¾' }

/** Hundredths as the API's canonical decimal: 150 → "1.5", 33 → "0.33", 200 → "2". */
export function decimalOf(hundredths: number): string {
  const h = Math.round(hundredths)
  const whole = Math.floor(h / HUNDREDTHS)
  const cents = h % HUNDREDTHS
  if (cents === 0) return String(whole)
  return `${whole}.${String(cents).padStart(2, '0')}`.replace(/0$/, '')
}

/** Parses the API's canonical decimal ("1.5", "0.33", "250") into hundredths; null for anything else. */
export function amountHundredths(decimal: string | null | undefined): number | null {
  if (!decimal) return null
  const m = /^(\d{1,7})(?:\.(\d{1,2}))?$/.exec(decimal.trim())
  if (!m?.[1]) return null
  const h = Number(m[1]) * HUNDREDTHS + Number((m[2] ?? '').padEnd(2, '0'))
  return h > 0 ? h : null
}

/**
 * The number for people: «1½», «⅓», «2⅔» for fraction-friendly units and a
 * bare number; a decimal comma otherwise («0,3», «1,5 г»). Pass decimal to
 * force the comma (metric amounts converted by the scaler: «1,5 л»).
 */
export function formatAmount(hundredths: number, unit: string | null, { decimal = false } = {}): string {
  const h = Math.round(hundredths)
  const whole = Math.floor(h / HUNDREDTHS)
  const vulgar = VULGAR_BY_HUNDREDTHS[h % HUNDREDTHS]
  if (vulgar && !decimal && isFractionFriendly(unit)) return whole === 0 ? vulgar : `${whole}${vulgar}`
  return decimalOf(h).replace('.', ',')
}

/** Amount and declined unit joined by a no-break space: «2 чайные ложки», «½ кг», «по вкусу», «3». */
export function formatHundredths(
  hundredths: number | null,
  unit: string | null,
  table?: UnitFormsTable | null,
  options?: { decimal?: boolean },
): string {
  const label = unitLabel(unit, hundredths, table)
  const amount = hundredths ? formatAmount(hundredths, unit, options) : ''
  if (amount === '') return label ?? ''
  return label ? `${amount}${NBSP}${label}` : amount
}

/** Like the server's Quantity.Format, from the API's amount and unit. */
export function formatQuantity(amount: string | null, unit: string | null, table?: UnitFormsTable | null): string {
  const h = amountHundredths(amount)
  if (h === null && amount) {
    // Not a canonical decimal (should not happen): show it as it came.
    const raw = amount.replace('.', ',')
    return unit ? `${raw}${NBSP}${unitLabel(unit, null, table) ?? unit}` : raw
  }
  return formatHundredths(h, unit, table)
}

// ------------------------------------------------------------ parsing --

/** The vulgar fractions accepted in amounts, as [numerator, denominator]. */
const VULGAR_FRACTIONS: Readonly<Record<string, readonly [number, number]>> = {
  '½': [1, 2],
  '⅓': [1, 3],
  '⅔': [2, 3],
  '¼': [1, 4],
  '¾': [3, 4],
  '⅕': [1, 5],
  '⅖': [2, 5],
  '⅗': [3, 5],
  '⅘': [4, 5],
  '⅒': [1, 10],
}

/** What the fraction chips in amount fields offer. */
export const FRACTION_CHIPS = ['¼', '⅓', '½', '⅔', '¾'] as const
export type FractionChip = (typeof FRACTION_CHIPS)[number]

const CHIP_HUNDREDTHS: Readonly<Record<FractionChip, number>> = { '¼': 25, '⅓': 33, '½': 50, '⅔': 67, '¾': 75 }

/** Words people type instead of a number. */
const AMOUNT_WORDS: Readonly<Record<string, number>> = {
  пол: 50,
  половина: 50,
  половинка: 50,
  полторы: 150,
  полтора: 150,
}

export const BAD_AMOUNT = 'Укажите дробь вида 1/2, 1/3, 1/4 или десятичную, например 0,5'

export type AmountParse = { ok: true; hundredths: number } | { ok: false; message: string }

const MAX_DIGITS = 6
const DENOMINATORS: ReadonlySet<number> = new Set([2, 3, 4, 5, 10])

function parseWhole(s: string): number | null {
  return s.length > 0 && s.length <= MAX_DIGITS && /^\d+$/.test(s) ? Number(s) : null
}

function parseDecimal(s: string): number | null {
  const normalised = s.replaceAll(',', '.')
  const dot = normalised.indexOf('.')
  const intPart = dot < 0 ? normalised : normalised.slice(0, dot)
  const frac = dot < 0 ? '' : normalised.slice(dot + 1)
  const whole = parseWhole(intPart)
  if (whole === null || !/^\d*$/.test(frac) || frac.length > 2) return null
  return whole * HUNDREDTHS + Number(frac.padEnd(2, '0'))
}

/** «n/d» into hundredths, rounding half up; proper requires n < d (after a whole part). */
function parseFraction(s: string, proper: boolean): number | null {
  const slash = s.indexOf('/')
  if (slash < 0) return null
  const num = parseWhole(s.slice(0, slash))
  const den = parseWhole(s.slice(slash + 1))
  if (num === null || den === null || (proper && num >= den) || !DENOMINATORS.has(den)) return null
  return Math.floor((num * 2 * HUNDREDTHS + den) / (2 * den))
}

/**
 * Reads an amount as the server's ParseQuantityAmount does: «0,5», «1.5»,
 * «250», «1/2», «3/4», «1 1/2», «½», «1½», «1 ½»; denominators 2, 3, 4, 5
 * and 10 (thirds round to 0.33 / 0.67). The client also understands «пол»
 * and «полторы» and always sends the canonical decimal.
 */
export function parseAmount(raw: string): AmountParse {
  const word = AMOUNT_WORDS[raw.trim().toLocaleLowerCase('ru')]
  if (word !== undefined) return { ok: true, hundredths: word }
  let s = ''
  for (const ch of raw) {
    const vulgar = VULGAR_FRACTIONS[ch]
    if (vulgar) s += ` ${vulgar[0]}/${vulgar[1]} `
    else if (ch === '⁄' || ch === '∕') s += '/'
    else if (/\s/u.test(ch)) s += ' '
    else s += ch
  }
  const fields = s.split(' ').filter((f) => f !== '')
  let h: number | null = null
  if (fields.length === 1 && fields[0] !== undefined) {
    h = fields[0].includes('/') ? parseFraction(fields[0], false) : parseDecimal(fields[0])
  } else if (fields.length === 2 && fields[0] !== undefined && fields[1] !== undefined) {
    const whole = parseWhole(fields[0])
    const frac = parseFraction(fields[1], true)
    h = whole !== null && frac !== null ? whole * HUNDREDTHS + frac : null
  }
  if (h === null) return { ok: false, message: BAD_AMOUNT }
  if (h <= 0) return { ok: false, message: 'Количество должно быть больше нуля' }
  if (h > MAX_HUNDREDTHS) return { ok: false, message: 'Слишком большое количество' }
  return { ok: true, hundredths: h }
}

/** The fraction chip an amount field currently ends in («1½» → ½), if any. */
export function chipOf(raw: string): FractionChip | null {
  const r = parseAmount(raw)
  if (!r.ok) return null
  const frac = r.hundredths % HUNDREDTHS
  return FRACTION_CHIPS.find((c) => CHIP_HUNDREDTHS[c] === frac) ?? null
}

/**
 * A fraction chip joins the whole part of what is typed: «1» + ½ → «1½»,
 * «1½» + ¼ → «1¼», «» + ¾ → «¾». Tapping the chip that is already there
 * leaves the whole part alone («1½» + ½ → «1»).
 */
export function joinFraction(raw: string, chip: FractionChip): string {
  const r = parseAmount(raw)
  const whole = r.ok ? Math.floor(r.hundredths / HUNDREDTHS) : 0
  const head = whole > 0 ? String(whole) : ''
  return chipOf(raw) === chip ? head : `${head}${chip}`
}

/** An API amount in an input field: «1½» for spoons and pieces, «1,5» for grams; '' without an amount. */
export function amountForEdit(amount: string | null, unit: string | null = null): string {
  const h = amountHundredths(amount)
  if (h === null) return amount ? amount.replace('.', ',') : ''
  return formatAmount(h, unit)
}

// -------------------------------------------------------------- units --

/** Common ways to write a unit besides its code and its forms, as the server's unitSpellings. */
const UNIT_SPELLINGS: Readonly<Record<string, readonly string[]>> = {
  г: ['гр', 'грамм', 'грамма', 'граммов', 'граммы'],
  кг: ['килограмм', 'килограмма', 'килограммов', 'килограммы', 'кило'],
  мл: ['миллилитр', 'миллилитра', 'миллилитров', 'миллилитры'],
  л: ['литр', 'литра', 'литров', 'литры'],
  шт: ['штук', 'штука', 'штуки', 'штуку'],
  'ст. л.': ['стл', 'ст ложка', 'ст ложки', 'ст ложек', 'ст ложку', 'столовую ложку'],
  'ч. л.': ['чл', 'ч ложка', 'ч ложки', 'ч ложек', 'ч ложку', 'чайную ложку'],
  стакан: ['стаканы'],
  щепотка: ['щепоть', 'щепотку'],
  зубчик: ['зуб', 'зубчики'],
  пучок: ['пучки'],
  упаковка: ['уп', 'упак', 'упаковку', 'пачка', 'пачки', 'пачек', 'пачку'],
}

/** Lower case, ё as е, dots, hyphens and runs of spaces as one space: «Ч.Л.», «ч. л.» and «ч л» meet. */
export function unitKey(raw: string): string {
  return raw
    .toLocaleLowerCase('ru')
    .replaceAll('ё', 'е')
    .replace(/[.\-\s]+/gu, ' ')
    .trim()
}

const UNIT_ALIASES: ReadonlyMap<string, string> = (() => {
  const out = new Map<string, string>()
  for (const code of UNIT_CODES) {
    const f = unitForms(code)
    for (const s of [code, f.one, f.few, f.many, f.fraction, ...(UNIT_SPELLINGS[code] ?? [])]) out.set(unitKey(s), code)
  }
  return out
})()

/**
 * Reads a unit as typed («ч.л.», «чайных ложек», «гр», «шт.») into its code,
 * '' for no unit, or null when it is not a unit (or not in Me.units).
 */
export function parseUnit(raw: string, units: readonly string[] = UNIT_CODES): string | null {
  const key = unitKey(raw)
  if (key === '') return ''
  const code = UNIT_ALIASES.get(key)
  return code !== undefined && units.includes(code) ? code : null
}

/** A unit picker option: the code as the value, the word declined for the typed amount as the label. */
export function unitOptionLabel(unit: string, amountRaw: string, table?: UnitFormsTable | null): string {
  const r = parseAmount(amountRaw)
  return unitLabel(unit, r.ok ? r.hundredths : null, table) ?? unit
}
