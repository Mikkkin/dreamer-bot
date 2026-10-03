import { describe, expect, test } from 'bun:test'
import type { Cook, Recipe, RecipeTag } from '../api/types'
import {
  countByTag,
  matchesTags,
  ratingOf,
  ratingTenths,
  recipeTagLine,
  recipeTags,
  sortRecipes,
  sortTags,
  tagMap,
  toggleCourse,
  withoutTag,
} from './recipes'

function recipe(id: number, patch: Partial<Recipe> = {}, cooking: Partial<Recipe['cooking']> = {}): Recipe {
  return {
    id,
    title: `r${id}`,
    link: null,
    body: '',
    cuisine_id: null,
    course_ids: [],
    ingredients: [],
    servings: null,
    nutrition: null,
    cooking: { count: 0, last_cooked_at: null, rating_avg: null, rating_count: 0, ...cooking },
    author: { id: 1, name: 'Дима' },
    images: [],
    created_at: `2026-09-${String(id).padStart(2, '0')}T10:00:00Z`,
    updated_at: `2026-09-${String(id).padStart(2, '0')}T10:00:00Z`,
    ...patch,
  }
}

const TAGS: RecipeTag[] = [
  { id: 9, kind: 'course', name: 'Ужин', emoji: '🌙', position: 2 },
  { id: 3, kind: 'cuisine', name: 'Итальянская', emoji: '🍝', position: 2 },
  { id: 11, kind: 'course', name: 'Первое', emoji: '🍲', position: 3 },
  { id: 1, kind: 'cuisine', name: 'Русская', emoji: '🥟', position: 0 },
]
const byId = tagMap(TAGS)

test('sortTags: cuisines first, then by position', () => {
  expect(sortTags(TAGS).map((t) => t.id)).toEqual([1, 3, 9, 11])
})

describe('sortRecipes', () => {
  test('«Новые»: newest first', () => {
    expect(sortRecipes([recipe(1), recipe(3), recipe(2)], 'new').map((r) => r.id)).toEqual([3, 2, 1])
  })

  test('«Лучшие»: by average, unrated last, then by the number of ratings and cookings', () => {
    const list = [
      recipe(1, {}, { rating_avg: '4.5', rating_count: 2, count: 1 }),
      recipe(2),
      recipe(3, {}, { rating_avg: '5', rating_count: 1, count: 1 }),
      recipe(4, {}, { rating_avg: '4.5', rating_count: 4, count: 3 }),
      recipe(5, {}, { count: 2 }),
      recipe(6, {}, { rating_avg: '4.5', rating_count: 4, count: 5 }),
    ]
    expect(sortRecipes(list, 'best').map((r) => r.id)).toEqual([3, 6, 4, 1, 5, 2])
  })

  test('«Давно не готовили»: the longest wait first, never cooked counts from the day it was saved', () => {
    const list = [
      recipe(20, {}, { count: 1, last_cooked_at: '2026-09-25T10:00:00Z' }),
      recipe(2, {}, { count: 1, last_cooked_at: '2026-09-28T10:00:00Z' }),
      recipe(10),
      recipe(1),
      recipe(15, {}, { count: 4, last_cooked_at: '2026-08-01T10:00:00Z' }),
    ]
    expect(sortRecipes(list, 'stale').map((r) => r.id)).toEqual([15, 1, 10, 20, 2])
  })

  test('does not mutate the input', () => {
    const list = [recipe(1), recipe(2)]
    sortRecipes(list, 'new')
    expect(list.map((r) => r.id)).toEqual([1, 2])
  })
})

test('matchesTags filters by cuisine and course', () => {
  const r = recipe(1, { cuisine_id: 3, course_ids: [9, 11] })
  expect(matchesTags(r, 'all', 'all')).toBe(true)
  expect(matchesTags(r, 3, 'all')).toBe(true)
  expect(matchesTags(r, 1, 'all')).toBe(false)
  expect(matchesTags(r, 3, 11)).toBe(true)
  expect(matchesTags(r, 'all', 12)).toBe(false)
  expect(matchesTags(recipe(2), 3, 'all')).toBe(false)
})

test('countByTag', () => {
  const counts = countByTag([recipe(1, { cuisine_id: 3, course_ids: [9, 9] }), recipe(2, { cuisine_id: 3, course_ids: [11] }), recipe(3)])
  expect(counts).toEqual(
    new Map([
      [3, 2],
      [9, 1],
      [11, 1],
    ]),
  )
})

describe('tag line and chips', () => {
  test('cuisine emoji + first course', () => {
    expect(recipeTagLine(recipe(1, { cuisine_id: 3, course_ids: [9, 11] }), byId)).toBe('🍝 Ужин')
  })

  test('falls back to whichever tag exists', () => {
    expect(recipeTagLine(recipe(1, { cuisine_id: 3 }), byId)).toBe('🍝 Итальянская')
    expect(recipeTagLine(recipe(1, { course_ids: [11] }), byId)).toBe('🍲 Первое')
    expect(recipeTagLine(recipe(1), byId)).toBeNull()
  })

  test('unknown (deleted) tags are skipped', () => {
    expect(recipeTagLine(recipe(1, { cuisine_id: 99, course_ids: [98, 11] }), byId)).toBe('🍲 Первое')
    expect(recipeTags(recipe(1, { cuisine_id: 3, course_ids: [98, 11, 9] }), byId).map((t) => t.id)).toEqual([3, 11, 9])
  })
})

test('withoutTag strips a deleted tag', () => {
  const r = recipe(1, { cuisine_id: 3, course_ids: [9, 11] })
  expect(withoutTag(r, 3)).toMatchObject({ cuisine_id: null, course_ids: [9, 11] })
  expect(withoutTag(r, 9)).toMatchObject({ cuisine_id: 3, course_ids: [11] })
  expect(withoutTag(r, 42)).toBe(r)
})

test('ratingTenths', () => {
  expect(ratingTenths(recipe(1, {}, { rating_avg: '4.5' }))).toBe(45)
  expect(ratingTenths(recipe(1))).toBeNull()
})

test('ratingOf finds the person’s rating', () => {
  const cook: Cook = {
    id: 1,
    recipe_id: 1,
    cooked_by: { id: 1, name: 'Дима' },
    cooked_at: '2026-09-20T18:02:00Z',
    ratings: [{ user: { id: 2, name: 'Аня' }, stars: 4, comment: '', rated_at: '2026-09-20T19:00:00Z' }],
  }
  expect(ratingOf(cook, 2)?.stars).toBe(4)
  expect(ratingOf(cook, 1)).toBeUndefined()
})

describe('toggleCourse', () => {
  test('adds in the order of selection and removes', () => {
    expect(toggleCourse([9], 11, 8)).toEqual({ ids: [9, 11], limited: false })
    expect(toggleCourse([9, 11], 9, 8)).toEqual({ ids: [11], limited: false })
  })

  test('respects the limit', () => {
    expect(toggleCourse([1, 2], 3, 2)).toEqual({ ids: [1, 2], limited: true })
    expect(toggleCourse([1, 2], 2, 2)).toEqual({ ids: [1], limited: false })
  })
})
