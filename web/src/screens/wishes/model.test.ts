import { describe, expect, test } from 'bun:test'
import type { Wish } from '../../api/types'
import { countByCategory, countByStatus, inCategory, sortWishes } from './model'

function wish(id: number, patch: Partial<Wish> = {}): Wish {
  return {
    id,
    title: `w${id}`,
    note: '',
    category_id: null,
    link: null,
    price: null,
    status: 'want',
    hot: false,
    author: { id: 1, name: 'Дима' },
    images: [],
    created_at: `2026-09-${String(id).padStart(2, '0')}T10:00:00Z`,
    updated_at: `2026-09-${String(id).padStart(2, '0')}T10:00:00Z`,
    fulfilled_at: null,
    saved: null,
    ...patch,
  }
}

describe('sortWishes', () => {
  test('hot first, then newest', () => {
    const list = [wish(1), wish(2, { hot: true }), wish(3), wish(4, { hot: true })]
    expect(sortWishes(list, 'want').map((w) => w.id)).toEqual([4, 2, 3, 1])
  })

  test('fulfilled wishes by fulfilment date', () => {
    const list = [
      wish(1, { status: 'done', fulfilled_at: '2026-09-20T00:00:00Z' }),
      wish(2, { status: 'done', fulfilled_at: '2026-09-25T00:00:00.123456Z' }),
      wish(3, { status: 'done', fulfilled_at: null, updated_at: '2026-09-22T00:00:00Z' }),
    ]
    expect(sortWishes(list, 'done').map((w) => w.id)).toEqual([2, 3, 1])
  })
})

test('countByStatus and countByCategory', () => {
  const list = [wish(1), wish(2, { status: 'done', category_id: 5 }), wish(3, { status: 'progress', category_id: 5 })]
  expect(countByStatus(list)).toEqual({ want: 1, progress: 1, done: 1 })
  expect(countByCategory(list)).toEqual(
    new Map<number | null, number>([
      [null, 1],
      [5, 2],
    ]),
  )
})

test('inCategory', () => {
  expect(inCategory(wish(1), 'all')).toBe(true)
  expect(inCategory(wish(1), 'none')).toBe(true)
  expect(inCategory(wish(1, { category_id: 2 }), 'none')).toBe(false)
  expect(inCategory(wish(1, { category_id: 2 }), 2)).toBe(true)
  expect(inCategory(wish(1, { category_id: 2 }), 3)).toBe(false)
})
