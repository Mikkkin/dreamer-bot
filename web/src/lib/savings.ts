import type { Price, Wish } from '../api/types'
import { formatMoney, parseMinor, sumByCurrency, type CurrencyTotal } from './format'

// «Копим»: how much of a wish's price has been put aside.

export interface SavingsProgress {
  savedMinor: number
  /** null when the wish has no price in the savings currency. */
  priceMinor: number | null
  currency: string
  /** 0..100, floored like the server; null without a price. */
  percent: number | null
  /** Everything is saved (not just "99.6% rounded"). */
  complete: boolean
}

/** Percent of the price saved: floored and capped at 100, like domain.Wish.SavedPercent. */
export function savedPercent(savedMinor: number, priceMinor: number): number {
  if (priceMinor <= 0) return 0
  return Math.min(100, Math.floor((savedMinor * 100) / priceMinor))
}

export function savingsProgress(wish: Pick<Wish, 'price' | 'saved'>): SavingsProgress | null {
  const saved = wish.saved
  if (!saved) return null
  const savedMinor = parseMinor(saved.total.amount)
  if (savedMinor === null) return null
  const currency = saved.total.currency
  const price = wish.price && wish.price.currency === currency ? parseMinor(wish.price.amount) : null
  if (price === null || price <= 0) return { savedMinor, priceMinor: null, currency, percent: null, complete: false }
  return { savedMinor, priceMinor: price, currency, percent: savedPercent(savedMinor, price), complete: savedMinor >= price }
}

/**
 * The currency new savings must use: the price currency, otherwise the
 * currency already saved. Without either, the user may pick (locked=false).
 */
export function savingsCurrency(wish: Pick<Wish, 'price' | 'saved'>, fallback: string): { currency: string; locked: boolean } {
  const locked = wish.price?.currency ?? wish.saved?.total.currency
  return locked ? { currency: locked, locked: true } : { currency: fallback, locked: false }
}

const plainNumbers = new Map<number, Intl.NumberFormat>()

function plainNumber(minor: number): string {
  const digits = minor % 100 === 0 ? 0 : 2
  let f = plainNumbers.get(digits)
  if (!f) {
    f = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: digits, maximumFractionDigits: digits })
    plainNumbers.set(digits, f)
  }
  return f.format(minor / 100)
}

/** "12 000 / 45 000 ₽": the currency sign only once, after the target. */
export function formatSavedOfPrice(savedMinor: number, priceMinor: number, currency: string): string {
  return `${plainNumber(savedMinor)} / ${formatMoney(priceMinor, currency)}`
}

/** Money put aside for wishes that have not come true yet, per currency. */
export function openSavings(wishes: readonly Pick<Wish, 'status' | 'saved'>[]): CurrencyTotal[] {
  const totals: Price[] = []
  for (const w of wishes) if (w.status !== 'done' && w.saved) totals.push(w.saved.total)
  return sumByCurrency(totals)
}
