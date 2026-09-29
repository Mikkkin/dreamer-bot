import type { ShoppingItem } from '../api/types'

const time = (iso: string) => Date.parse(iso) || 0

/** The server's order: unchecked first, then checked; oldest first within each. */
export function sortShopping(items: readonly ShoppingItem[]): ShoppingItem[] {
  return [...items].sort((a, b) => Number(a.checked) - Number(b.checked) || time(a.created_at) - time(b.created_at) || a.id - b.id)
}

/** Replaces known items (a merge returns an existing id) and adds new ones. */
export function upsertShopping(list: readonly ShoppingItem[], items: readonly ShoppingItem[]): ShoppingItem[] {
  const byId = new Map(list.map((it) => [it.id, it]))
  for (const it of items) byId.set(it.id, it)
  return sortShopping([...byId.values()])
}

export function setChecked(list: readonly ShoppingItem[], id: number, checked: boolean): ShoppingItem[] {
  return sortShopping(list.map((it) => (it.id === id ? { ...it, checked } : it)))
}

export function shoppingCounts(items: readonly ShoppingItem[]): { open: number; checked: number } {
  let checked = 0
  for (const it of items) if (it.checked) checked += 1
  return { open: items.length - checked, checked }
}

/**
 * Orders concurrent toggles of one item: only the response to the latest
 * request may update the list, so a slow early reply never undoes a later tap.
 */
export class LatestRequest {
  #seq = new Map<number, number>()

  begin(id: number): number {
    const n = (this.#seq.get(id) ?? 0) + 1
    this.#seq.set(id, n)
    return n
  }

  isLatest(id: number, n: number): boolean {
    return this.#seq.get(id) === n
  }
}
