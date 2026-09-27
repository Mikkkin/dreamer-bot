// Russian formatting of money, counts and dates.

/** Display order of currencies; matches the server's domain.Currencies. */
export const CURRENCY_ORDER: readonly string[] = ['EUR', 'USD', 'RUB', 'GBP']

const MINOR_PER_UNIT = 100
const DECIMAL_RE = /^(\d{1,13})(?:\.(\d{1,2}))?$/

/** Parses a canonical API amount such as "1200.50" into minor units (cents). */
export function parseMinor(decimal: string): number | null {
  const m = DECIMAL_RE.exec(decimal.trim())
  if (!m) return null
  const units = Number(m[1])
  const frac = Number((m[2] ?? '').padEnd(2, '0'))
  const minor = units * MINOR_PER_UNIT + frac
  return Number.isSafeInteger(minor) ? minor : null
}

const moneyFormats = new Map<string, Intl.NumberFormat>()

function moneyFormat(currency: string, fractionDigits: number): Intl.NumberFormat {
  const key = `${currency}:${fractionDigits}`
  let f = moneyFormats.get(key)
  if (!f) {
    f = new Intl.NumberFormat('ru-RU', {
      style: 'currency',
      currency,
      minimumFractionDigits: fractionDigits,
      maximumFractionDigits: fractionDigits,
    })
    moneyFormats.set(key, f)
  }
  return f
}

/** "1 200 €", "1 200,50 €" — kopecks/cents are shown only when present, like the server. */
export function formatMoney(minor: number, currency: string): string {
  const whole = minor % MINOR_PER_UNIT === 0
  try {
    return moneyFormat(currency, whole ? 0 : 2).format(minor / MINOR_PER_UNIT)
  } catch {
    return `${(minor / MINOR_PER_UNIT).toFixed(whole ? 0 : 2)} ${currency}`
  }
}

export interface CurrencyTotal {
  currency: string
  minor: number
  count: number
}

/** Sums amounts per currency without conversion, ordered like CURRENCY_ORDER. */
export function sumByCurrency(amounts: Iterable<{ amount: string; currency: string }>): CurrencyTotal[] {
  const totals = new Map<string, CurrencyTotal>()
  for (const a of amounts) {
    const minor = parseMinor(a.amount)
    if (minor === null) continue
    const t = totals.get(a.currency) ?? { currency: a.currency, minor: 0, count: 0 }
    t.minor += minor
    t.count += 1
    totals.set(a.currency, t)
  }
  return [...totals.values()].sort((x, y) => currencyRank(x.currency) - currencyRank(y.currency))
}

function currencyRank(c: string): number {
  const i = CURRENCY_ORDER.indexOf(c)
  return i < 0 ? CURRENCY_ORDER.length : i
}

export type PluralForms = readonly [one: string, few: string, many: string]

const pluralRules = new Intl.PluralRules('ru-RU')

/** Picks the Russian plural form: 1 желание, 2 желания, 5 желаний. */
export function plural(n: number, forms: PluralForms): string {
  switch (pluralRules.select(n)) {
    case 'one':
      return forms[0]
    case 'few':
      return forms[1]
    default:
      return forms[2]
  }
}

export function countOf(n: number, forms: PluralForms): string {
  return `${n} ${plural(n, forms)}`
}

export const WISH_FORMS: PluralForms = ['желание', 'желания', 'желаний']
export const RECIPE_FORMS: PluralForms = ['рецепт', 'рецепта', 'рецептов']
export const DREAM_FORMS: PluralForms = ['мечту', 'мечты', 'мечтаний']
export const PHOTO_FORMS: PluralForms = ['фото', 'фото', 'фото']

const dayMonth = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long' })
const dayMonthYear = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' })
const dayMonthShort = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'short' })

function toDate(iso: string): Date | null {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? null : d
}

/** "12 сентября", or "12 сентября 2025 г." for another year. */
export function formatDay(iso: string, now: Date = new Date()): string {
  const d = toDate(iso)
  if (!d) return ''
  return (d.getFullYear() === now.getFullYear() ? dayMonth : dayMonthYear).format(d)
}

/** "12 сент." */
export function formatShortDay(iso: string): string {
  const d = toDate(iso)
  return d ? dayMonthShort.format(d) : ''
}

/** "Дима · 12 сентября" — neutral wording, no gendered verbs. */
export function formatByline(name: string, iso: string, now: Date = new Date()): string {
  const day = formatDay(iso, now)
  return day ? `${name} · ${day}` : name
}

export function percent(part: number, total: number): number {
  return total <= 0 ? 0 : Math.round((part / total) * 100)
}

/** Genitive after "из": из 1 рецепта, из 3 рецептов, из 21 рецепта. */
export const RECIPE_GENITIVE_FORMS: PluralForms = ['рецепта', 'рецептов', 'рецептов']
