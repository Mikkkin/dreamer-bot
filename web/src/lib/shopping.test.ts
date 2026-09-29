import { describe, expect, test } from 'bun:test'
import type { ShoppingItem } from '../api/types'
import { LatestRequest, setChecked, shoppingCounts, sortShopping, upsertShopping } from './shopping'

function item(id: number, patch: Partial<ShoppingItem> = {}): ShoppingItem {
  return {
    id,
    name: `i${id}`,
    quantity: null,
    checked: false,
    recipe_id: null,
    added_by: { id: 1, name: 'Дима' },
    created_at: `2026-09-${String(id).padStart(2, '0')}T10:00:00Z`,
    updated_at: `2026-09-${String(id).padStart(2, '0')}T10:00:00Z`,
    ...patch,
  }
}

test('sortShopping: unchecked first, oldest first', () => {
  const list = [item(3, { checked: true }), item(2), item(1, { checked: true }), item(4)]
  expect(sortShopping(list).map((i) => i.id)).toEqual([2, 4, 1, 3])
})

describe('upsertShopping', () => {
  test('a merged item replaces the old one in place', () => {
    const merged = item(2, { quantity: { amount: '750', unit: 'мл', formatted: '750 мл' } })
    const out = upsertShopping([item(1), item(2)], [merged, item(5)])
    expect(out.map((i) => i.id)).toEqual([1, 2, 5])
    expect(out[1]?.quantity?.formatted).toBe('750 мл')
  })
})

test('setChecked moves the item between groups', () => {
  const out = setChecked([item(1), item(2), item(3)], 1, true)
  expect(out.map((i) => [i.id, i.checked])).toEqual([
    [2, false],
    [3, false],
    [1, true],
  ])
})

test('shoppingCounts', () => {
  expect(shoppingCounts([item(1), item(2, { checked: true }), item(3)])).toEqual({ open: 2, checked: 1 })
  expect(shoppingCounts([])).toEqual({ open: 0, checked: 0 })
})

test('LatestRequest lets only the last reply through', () => {
  const seq = new LatestRequest()
  const first = seq.begin(7)
  const second = seq.begin(7)
  const other = seq.begin(8)
  expect(seq.isLatest(7, first)).toBe(false)
  expect(seq.isLatest(7, second)).toBe(true)
  expect(seq.isLatest(8, other)).toBe(true)
})
