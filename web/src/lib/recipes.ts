import type { Cook, Recipe, RecipeTag, TagKind } from '../api/types'
import { apiTenths } from './nutrition'

export type RecipeSort = 'new' | 'best' | 'stale'

export const RECIPE_SORTS: readonly { value: RecipeSort; label: string }[] = [
  { value: 'new', label: 'Новые' },
  { value: 'best', label: 'Лучшие ⭐' },
  { value: 'stale', label: 'Давно не готовили' },
]

/** "all" shows every recipe, a number one tag. */
export type TagFilter = 'all' | number

const time = (iso: string | null | undefined) => (iso ? Date.parse(iso) || 0 : 0)

/** Tags in display order: cuisines, then courses, each by position. */
export function sortTags(tags: readonly RecipeTag[]): RecipeTag[] {
  const rank = (k: TagKind) => (k === 'cuisine' ? 0 : 1)
  return [...tags].sort((a, b) => rank(a.kind) - rank(b.kind) || a.position - b.position || a.id - b.id)
}

export function tagMap(tags: readonly RecipeTag[]): Map<number, RecipeTag> {
  return new Map(tags.map((t) => [t.id, t]))
}

export function tagsOfKind(tags: readonly RecipeTag[], kind: TagKind): RecipeTag[] {
  return tags.filter((t) => t.kind === kind)
}

export function matchesTags(recipe: Recipe, cuisine: TagFilter, course: TagFilter): boolean {
  return (cuisine === 'all' || recipe.cuisine_id === cuisine) && (course === 'all' || recipe.course_ids.includes(course))
}

/** How many recipes carry each tag. */
export function countByTag(recipes: readonly Recipe[]): Map<number, number> {
  const counts = new Map<number, number>()
  const bump = (id: number) => counts.set(id, (counts.get(id) ?? 0) + 1)
  for (const r of recipes) {
    if (r.cuisine_id !== null) bump(r.cuisine_id)
    for (const id of new Set(r.course_ids)) bump(id)
  }
  return counts
}

/** The average rating in tenths of a star (4.5 → 45), or null before the first rating. */
export function ratingTenths(recipe: Recipe): number | null {
  return recipe.cooking.rating_avg === null ? null : apiTenths(recipe.cooking.rating_avg)
}

/**
 * «Новые»: newest first. «Лучшие ⭐»: by average rating, rated first, then by
 * how often it was rated and cooked. «Давно не готовили»: the longest wait
 * first — since the last cooking, or since it was saved if never cooked.
 */
export function sortRecipes(recipes: readonly Recipe[], sort: RecipeSort): Recipe[] {
  const newest = (a: Recipe, b: Recipe) => time(b.created_at) - time(a.created_at) || b.id - a.id
  const list = [...recipes]
  switch (sort) {
    case 'new':
      return list.sort(newest)
    case 'best':
      return list.sort((a, b) => {
        const ra = ratingTenths(a)
        const rb = ratingTenths(b)
        if (ra !== rb) return rb === null ? -1 : ra === null ? 1 : rb - ra
        return b.cooking.rating_count - a.cooking.rating_count || b.cooking.count - a.cooking.count || newest(a, b)
      })
    case 'stale': {
      const waiting = (r: Recipe) => time(r.cooking.last_cooked_at ?? r.created_at)
      return list.sort((a, b) => waiting(a) - waiting(b) || a.id - b.id)
    }
  }
}

/** The card's tag line: cuisine emoji + first course («🍝 Ужин»), or whichever exists. */
export function recipeTagLine(recipe: Recipe, byId: ReadonlyMap<number, RecipeTag>): string | null {
  const cuisine = recipe.cuisine_id === null ? undefined : byId.get(recipe.cuisine_id)
  const course = recipe.course_ids.map((id) => byId.get(id)).find((t) => t !== undefined)
  if (cuisine && course) return `${cuisine.emoji} ${course.name}`
  if (cuisine) return `${cuisine.emoji} ${cuisine.name}`
  if (course) return `${course.emoji} ${course.name}`
  return null
}

/** The recipe's tags for chips: the cuisine first, then its courses in order. */
export function recipeTags(recipe: Recipe, byId: ReadonlyMap<number, RecipeTag>): RecipeTag[] {
  const out: RecipeTag[] = []
  const cuisine = recipe.cuisine_id === null ? undefined : byId.get(recipe.cuisine_id)
  if (cuisine) out.push(cuisine)
  for (const id of recipe.course_ids) {
    const t = byId.get(id)
    if (t && !out.includes(t)) out.push(t)
  }
  return out
}

/** A recipe as it is after the tag was deleted on the server. */
export function withoutTag(recipe: Recipe, tagId: number): Recipe {
  if (recipe.cuisine_id !== tagId && !recipe.course_ids.includes(tagId)) return recipe
  return {
    ...recipe,
    cuisine_id: recipe.cuisine_id === tagId ? null : recipe.cuisine_id,
    course_ids: recipe.course_ids.filter((id) => id !== tagId),
  }
}

export function ratingOf(cook: Cook, userId: number) {
  return cook.ratings.find((r) => r.user.id === userId)
}

/** Selects or deselects a course, keeping the order of selection and the per-recipe limit. */
export function toggleCourse(ids: readonly number[], id: number, max: number): { ids: number[]; limited: boolean } {
  if (ids.includes(id)) return { ids: ids.filter((x) => x !== id), limited: false }
  if (ids.length >= max) return { ids: [...ids], limited: true }
  return { ids: [...ids, id], limited: false }
}
