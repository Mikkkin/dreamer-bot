import type { Category } from '../api/types'

// Category art, in the spirit of Telegram gift backdrops: a radial two-tone
// backdrop (centre → edge) under a sticker emoji. Default categories get a fixed
// pair by emoji; custom ones get a stable pair derived from the id. Backdrops
// never carry text: white on the edge colours is only 2.2–4.3:1.

export interface Backdrop {
  center: string
  edge: string
}

const BACKDROPS: readonly Backdrop[] = [
  { center: '#FFC2D1', edge: '#F2557A' }, // rose — Покупки
  { center: '#9EDBFF', edge: '#2D8CF0' }, // sky — Путешествия
  { center: '#DCC2FF', edge: '#8E5CF0' }, // lilac — Впечатления
  { center: '#FFE0B0', edge: '#EE8F45' }, // apricot — Для дома
  { center: '#FFE48F', edge: '#EB7F1E' }, // saffron — Рестораны
  { center: '#FFC4DE', edge: '#D9468A' }, // berry — Подарки
  { center: '#B3F2CF', edge: '#22A866' }, // mint — Развитие
  { center: '#CBD0FF', edge: '#5E6BE6' }, // periwinkle — Другое
]

const BY_EMOJI: Readonly<Record<string, number>> = {
  '🛍': 0,
  '✈': 1,
  '🎉': 2,
  '🏠': 3,
  '🍽': 4,
  '🎁': 5,
  '📚': 6,
  '💫': 7,
}

const NEUTRAL = 7

function backdropIndex(category: Category | null | undefined): number {
  if (!category) return NEUTRAL
  const byEmoji = BY_EMOJI[category.emoji.replace(/️/g, '')]
  if (byEmoji !== undefined) return byEmoji
  return Math.abs(Math.imul(category.id, 2654435761)) % BACKDROPS.length
}

export function categoryBackdrop(category: Category | null | undefined): Backdrop {
  return BACKDROPS[backdropIndex(category)] ?? BACKDROPS[NEUTRAL]!
}

/** A flat gradient of the same pair, for small tiles and bars. */
export function categoryGradient(category: Category | null | undefined): string {
  const { center, edge } = categoryBackdrop(category)
  return `linear-gradient(135deg, ${center}, ${edge})`
}

export const RECIPE_BACKDROP: Backdrop = { center: '#FFD9B0', edge: '#EE6A4A' }
export const RECIPE_GRADIENT = `linear-gradient(135deg, ${RECIPE_BACKDROP.center}, ${RECIPE_BACKDROP.edge})`
export const UNCATEGORIZED_EMOJI = '💫'

export function categoryEmoji(category: Category | null | undefined): string {
  return category?.emoji ?? UNCATEGORIZED_EMOJI
}
