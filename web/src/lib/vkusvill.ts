import type { ShoppingItem, VkusvillCartLine, VkusvillMatch } from '../api/types'
import { isSafeHttpUrl } from './links'
import { parseMinor } from './format'

// «Собрать корзину во ВкусВилле»: the user confirms or swaps the matched
// products and quantities; the server builds a shared basket from the lines.

export const MAX_CART_LINES = 30
const HUNDREDTHS = 100
const MIN_QTY = 1 // 0.01
const MAX_QTY = 40 * HUNDREDTHS

/** One list item's choice: a product (null = do not add) and its quantity as typed. */
export interface CartChoice {
  xmlId: number | null
  quantity: string
}

export type CartChoices = ReadonlyMap<number, CartChoice>

export type CartQuantity = { ok: true; quantity: string; hundredths: number } | { ok: false; message: string }

/** Parses 0.01–40 with at most two decimals ("1,5" → "1.5"). */
export function parseCartQuantity(raw: string): CartQuantity {
  const s = raw.replace(/\s/gu, '').replace(',', '.')
  const m = /^(\d{1,3})(?:\.(\d{1,2}))?$/.exec(s)
  if (!m?.[1]) return { ok: false, message: 'Количество — число, например 2' }
  const h = Number(m[1]) * HUNDREDTHS + Number((m[2] ?? '').padEnd(2, '0'))
  if (h < MIN_QTY) return { ok: false, message: 'Количество больше нуля' }
  if (h > MAX_QTY) return { ok: false, message: 'Не больше 40' }
  return { ok: true, quantity: canonical(h), hundredths: h }
}

function canonical(h: number): string {
  const units = Math.floor(h / HUNDREDTHS)
  const frac = h % HUNDREDTHS
  return frac === 0 ? String(units) : `${units}.${String(frac).padStart(2, '0')}`.replace(/0$/, '')
}

/** Kilograms step by 0.1, everything else by 1; the result stays within 0.01–40. */
export function stepCartQuantity(raw: string, dir: 1 | -1, unit: string | null): string {
  const step = unit === 'кг' ? 10 : HUNDREDTHS
  const parsed = parseCartQuantity(raw)
  const current = parsed.ok ? parsed.hundredths : step
  const next = Math.min(MAX_QTY, Math.max(step, current + dir * step))
  return canonical(next).replace('.', ',')
}

/** Pieces and packs from the list carry over; anything else starts at 1. */
function defaultQuantity(item: ShoppingItem | undefined): string {
  const q = item?.quantity
  if (!q?.amount || (q.unit !== 'шт' && q.unit !== 'упаковка')) return '1'
  const parsed = parseCartQuantity(q.amount)
  return parsed.ok && Number.isInteger(parsed.hundredths / HUNDREDTHS) ? parsed.quantity : '1'
}

/** The best candidate of every match, preselected. */
export function initialChoices(matches: readonly VkusvillMatch[], items: readonly ShoppingItem[]): Map<number, CartChoice> {
  const byId = new Map(items.map((it) => [it.id, it]))
  const out = new Map<number, CartChoice>()
  for (const m of matches) {
    out.set(m.item_id, { xmlId: m.candidates[0]?.xml_id ?? null, quantity: defaultQuantity(byId.get(m.item_id)) })
  }
  return out
}

export type CartLinesResult = { ok: true; lines: VkusvillCartLine[] } | { ok: false; errors: Map<number, string>; message?: string }

/** Validates the choices: only offered products, sane quantities, 1–30 lines. */
export function cartLines(matches: readonly VkusvillMatch[], choices: CartChoices): CartLinesResult {
  const errors = new Map<number, string>()
  const lines: VkusvillCartLine[] = []
  for (const m of matches) {
    const choice = choices.get(m.item_id)
    if (!choice || choice.xmlId === null) continue
    if (!m.candidates.some((c) => c.xml_id === choice.xmlId)) {
      errors.set(m.item_id, 'Выберите товар из списка')
      continue
    }
    const q = parseCartQuantity(choice.quantity)
    if (!q.ok) {
      errors.set(m.item_id, q.message)
      continue
    }
    lines.push({ xml_id: choice.xmlId, quantity: q.quantity })
  }
  if (errors.size > 0) return { ok: false, errors }
  if (lines.length === 0) return { ok: false, errors, message: 'Выберите хотя бы один товар' }
  if (lines.length > MAX_CART_LINES) return { ok: false, errors, message: `Не больше ${MAX_CART_LINES} товаров за раз` }
  return { ok: true, lines }
}

export interface CartEstimate {
  minor: number
  currency: string
  /** false when a chosen product has no price: the sum is a lower bound. */
  complete: boolean
  count: number
}

/** The approximate basket total of the valid choices (prices change, so it is only a guide). */
export function estimateCart(matches: readonly VkusvillMatch[], choices: CartChoices): CartEstimate | null {
  let minor = 0
  let count = 0
  let complete = true
  let currency: string | null = null
  for (const m of matches) {
    const choice = choices.get(m.item_id)
    if (!choice || choice.xmlId === null) continue
    const product = m.candidates.find((c) => c.xml_id === choice.xmlId)
    const q = parseCartQuantity(choice.quantity)
    if (!product || !q.ok) continue
    count += 1
    const price = product.price ? parseMinor(product.price.amount) : null
    if (!product.price || price === null || (currency !== null && product.price.currency !== currency)) {
      complete = false
      continue
    }
    currency = product.price.currency
    minor += Math.round((price * q.hundredths) / HUNDREDTHS)
  }
  if (count === 0) return null
  return { minor, currency: currency ?? 'RUB', complete, count }
}

/** The basket link the server returned: https on vkusvill.ru only. */
export function isVkusvillBasketUrl(url: string): boolean {
  if (!isSafeHttpUrl(url)) return false
  try {
    const u = new URL(url)
    return u.protocol === 'https:' && (u.hostname === 'vkusvill.ru' || u.hostname.endsWith('.vkusvill.ru')) && u.port === ''
  } catch {
    return false
  }
}
