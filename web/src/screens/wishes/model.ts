import type { Category, Status, Wish } from '../../api/types'

export const STATUS_LABEL: Readonly<Record<Status, string>> = {
  want: 'Хотим',
  progress: 'Копим',
  done: 'Сбылось ✨',
}

/** "all" shows every wish, "none" the uncategorised ones, a number one category. */
export type CategoryFilter = 'all' | 'none' | number

export function inCategory(wish: Wish, filter: CategoryFilter): boolean {
  if (filter === 'all') return true
  if (filter === 'none') return wish.category_id === null
  return wish.category_id === filter
}

export function countByStatus(wishes: readonly Wish[]): Record<Status, number> {
  const counts: Record<Status, number> = { want: 0, progress: 0, done: 0 }
  for (const w of wishes) counts[w.status] += 1
  return counts
}

/** Counts per category id; uncategorised wishes are counted under null. */
export function countByCategory(wishes: readonly Wish[]): Map<number | null, number> {
  const counts = new Map<number | null, number>()
  for (const w of wishes) counts.set(w.category_id, (counts.get(w.category_id) ?? 0) + 1)
  return counts
}

const time = (iso: string | null) => (iso ? Date.parse(iso) || 0 : 0)

/** Hot first, then newest; fulfilled wishes by the date they came true. */
export function sortWishes(wishes: readonly Wish[], status: Status): Wish[] {
  return [...wishes].sort((a, b) =>
    status === 'done'
      ? time(b.fulfilled_at ?? b.updated_at) - time(a.fulfilled_at ?? a.updated_at)
      : Number(b.hot) - Number(a.hot) || time(b.created_at) - time(a.created_at),
  )
}

export function categoryMap(categories: readonly Category[]): Map<number, Category> {
  return new Map(categories.map((c) => [c.id, c]))
}

export function categoryOf(wish: Wish, byId: ReadonlyMap<number, Category>): Category | undefined {
  return wish.category_id === null ? undefined : byId.get(wish.category_id)
}
