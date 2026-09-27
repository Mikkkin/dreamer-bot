// Client-side twin of the server's domain.ParseAmount, so the form can show a
// precise error before the request and always sends a canonical "1200.50".

const MAX_INT_DIGITS = 12
const MAX_MINOR = 1_000_000_000 * 100

export type AmountResult = { ok: true; decimal: string; minor: number } | { ok: false; message: string }

/**
 * Parses a human-typed amount such as "1200", "1 200,50", "1,200.50" or
 * "1.200,5". The last separator followed by one or two digits is the decimal
 * separator; any other '.' or ',' is a thousands separator.
 */
export function parseAmountInput(raw: string): AmountResult {
  const s = raw.replace(/[\s'_]/gu, '')
  if (s === '') return { ok: false, message: 'Укажите сумму' }

  const [rawInt, frac] = splitDecimal(s)
  const intPart = rawInt.replace(/[.,]/g, '')
  if (intPart === '' || !/^\d+$/.test(intPart) || !/^\d*$/.test(frac) || frac.length > 2) {
    return { ok: false, message: 'Не получилось распознать сумму' }
  }
  if (intPart.length > MAX_INT_DIGITS) return { ok: false, message: 'Слишком большая сумма' }

  const units = Number(intPart)
  const cents = frac === '' ? 0 : Number(frac.padEnd(2, '0'))
  const minor = units * 100 + cents
  if (minor <= 0) return { ok: false, message: 'Сумма должна быть больше нуля' }
  if (minor > MAX_MINOR) return { ok: false, message: 'Слишком большая сумма' }

  const decimal = cents === 0 ? String(units) : `${units}.${String(cents).padStart(2, '0')}`
  return { ok: true, decimal, minor }
}

function splitDecimal(s: string): [string, string] {
  const idx = Math.max(s.lastIndexOf('.'), s.lastIndexOf(','))
  if (idx < 0) return [s, '']
  const sep = s.charAt(idx)
  const other = sep === ',' ? '.' : ','
  const digitsAfter = s.length - idx - 1
  const mixed = s.includes(other)
  const single = s.split(sep).length === 2
  if (mixed || (single && digitsAfter <= 2)) return [s.slice(0, idx), s.slice(idx + 1)]
  return [s, '']
}

/** Renders a canonical API amount for an input field in Russian style: "1200.50" → "1200,50". */
export function amountForInput(decimal: string): string {
  return decimal.replace('.', ',')
}
