import { describe, expect, test } from 'bun:test'
import type { Me, Recipe, RecipeTag, ShoppingItem } from '../api/types'
import { initialState, reducer, type State } from './store'

const me: Me = {
  user: { id: 1, name: 'Дима' },
  partners: [{ id: 2, name: 'Аня' }],
  currencies: [],
  default_currency: 'RUB',
  limits: {} as Me['limits'],
  units: ['г'],
}

function recipe(id: number, patch: Partial<Recipe> = {}): Recipe {
  return {
    id,
    title: `r${id}`,
    link: null,
    body: '',
    cuisine_id: null,
    course_ids: [],
    ingredients: [],
    nutrition: null,
    cooking: { count: 0, last_cooked_at: null, rating_avg: null, rating_count: 0 },
    author: me.user,
    images: [],
    created_at: '2026-09-01T10:00:00Z',
    updated_at: '2026-09-01T10:00:00Z',
    ...patch,
  }
}

function item(id: number, patch: Partial<ShoppingItem> = {}): ShoppingItem {
  return {
    id,
    name: `i${id}`,
    quantity: null,
    checked: false,
    recipe_id: null,
    added_by: me.user,
    created_at: `2026-09-0${id}T10:00:00Z`,
    updated_at: `2026-09-0${id}T10:00:00Z`,
    ...patch,
  }
}

const tag = (id: number, kind: RecipeTag['kind'], position: number): RecipeTag => ({ id, kind, name: `t${id}`, emoji: '🍝', position })

function loaded(patch: Partial<State> = {}): State {
  const s = reducer(initialState, {
    type: 'loaded',
    me,
    wishes: [],
    recipes: [recipe(1, { cuisine_id: 3, course_ids: [9, 11] }), recipe(2, { course_ids: [9] })],
    categories: [],
    tags: [tag(9, 'course', 0), tag(3, 'cuisine', 0), tag(11, 'course', 1)],
    shopping: [item(2, { checked: true }), item(1), item(3, { recipe_id: 1 })],
  })
  return { ...s, ...patch }
}

test('loaded sorts tags and the shopping list', () => {
  const s = loaded()
  expect(s.phase).toBe('ready')
  expect(s.tags.map((t) => t.id)).toEqual([3, 9, 11])
  expect(s.shopping.map((i) => i.id)).toEqual([1, 3, 2])
})

test('drop-tag removes the tag from every recipe', () => {
  const s = reducer(loaded(), { type: 'drop-tag', id: 9 })
  expect(s.tags.map((t) => t.id)).toEqual([3, 11])
  expect(s.recipes.map((r) => r.course_ids)).toEqual([[11], []])
  const c = reducer(s, { type: 'drop-tag', id: 3 })
  expect(c.recipes[0]?.cuisine_id).toBeNull()
})

test('drop-recipe keeps shopping items but forgets their source', () => {
  const s = reducer(loaded(), { type: 'drop-recipe', id: 1 })
  expect(s.recipes.map((r) => r.id)).toEqual([2])
  expect(s.shopping.find((i) => i.id === 3)?.recipe_id).toBeNull()
})

describe('shopping', () => {
  test('put-shopping merges and keeps the order', () => {
    const s = reducer(loaded(), { type: 'put-shopping', items: [item(1, { checked: true }), item(4)] })
    expect(s.shopping.map((i) => [i.id, i.checked])).toEqual([
      [3, false],
      [4, false],
      [1, true],
      [2, true],
    ])
  })

  test('drop-shopping', () => {
    expect(reducer(loaded(), { type: 'drop-shopping', ids: [1, 2] }).shopping.map((i) => i.id)).toEqual([3])
  })
})

test('a failed refresh keeps the data and shows the error', () => {
  const error = { code: 'network' } as never
  const s = reducer(loaded(), { type: 'failed', error })
  expect(s.phase).toBe('ready')
  expect(s.error).toBe(error)
  expect(reducer(initialState, { type: 'failed', error }).phase).toBe('failed')
})

test('put-tag keeps tags ordered', () => {
  const s = reducer(loaded(), { type: 'put-tag', tag: tag(20, 'cuisine', 5) })
  expect(s.tags.map((t) => t.id)).toEqual([3, 20, 9, 11])
})
