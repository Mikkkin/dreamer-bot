import type { ApiError } from '../api/errors'
import type { Category, Me, Recipe, RecipeTag, ShoppingItem, Wish } from '../api/types'
import { sortTags, withoutTag } from '../lib/recipes'
import { sortShopping, upsertShopping } from '../lib/shopping'

// The client-side copy of the couple's data and how mutations change it.
// Pure, so it can be tested without React.

export interface State {
  phase: 'loading' | 'ready' | 'failed'
  me: Me | null
  wishes: Wish[]
  recipes: Recipe[]
  categories: Category[]
  /** Cuisine and course tags of recipes, cuisines first. */
  tags: RecipeTag[]
  /** The shared shopping list: unchecked first, oldest first. */
  shopping: ShoppingItem[]
  /** A failed load or refresh that the user can retry from a banner. */
  error: ApiError | null
}

export interface Loaded {
  me: Me
  wishes: Wish[]
  recipes: Recipe[]
  categories: Category[]
  tags: RecipeTag[]
  shopping: ShoppingItem[]
}

export type Action =
  | { type: 'loading' }
  | ({ type: 'loaded' } & Loaded)
  | { type: 'failed'; error: ApiError }
  | { type: 'wishes'; wishes: Wish[] }
  | { type: 'recipes'; recipes: Recipe[] }
  | { type: 'categories'; categories: Category[] }
  | { type: 'tags'; tags: RecipeTag[] }
  | { type: 'shopping'; items: ShoppingItem[] }
  | { type: 'put-wish'; wish: Wish }
  | { type: 'drop-wish'; id: number }
  | { type: 'put-recipe'; recipe: Recipe }
  | { type: 'drop-recipe'; id: number }
  | { type: 'put-category'; category: Category }
  | { type: 'drop-category'; id: number }
  | { type: 'put-tag'; tag: RecipeTag }
  | { type: 'drop-tag'; id: number }
  | { type: 'put-shopping'; items: ShoppingItem[] }
  | { type: 'drop-shopping'; ids: number[] }

export const initialState: State = {
  phase: 'loading',
  me: null,
  wishes: [],
  recipes: [],
  categories: [],
  tags: [],
  shopping: [],
  error: null,
}

function upsert<T extends { id: number }>(list: T[], item: T): T[] {
  const i = list.findIndex((x) => x.id === item.id)
  return i < 0 ? [item, ...list] : list.map((x) => (x.id === item.id ? item : x))
}

function sortCategories(list: Category[]): Category[] {
  return [...list].sort((a, b) => a.position - b.position || a.id - b.id)
}

export function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'loading':
      return { ...state, phase: state.phase === 'ready' ? 'ready' : 'loading' }
    case 'loaded':
      return {
        ...state,
        phase: 'ready',
        me: action.me,
        wishes: action.wishes,
        recipes: action.recipes,
        categories: sortCategories(action.categories),
        tags: sortTags(action.tags),
        shopping: sortShopping(action.shopping),
        error: null,
      }
    case 'failed':
      return { ...state, phase: state.phase === 'ready' ? 'ready' : 'failed', error: action.error }
    case 'wishes':
      return { ...state, wishes: action.wishes, error: null }
    case 'recipes':
      return { ...state, recipes: action.recipes, error: null }
    case 'categories':
      return { ...state, categories: sortCategories(action.categories), error: null }
    case 'tags':
      return { ...state, tags: sortTags(action.tags), error: null }
    case 'shopping':
      return { ...state, shopping: sortShopping(action.items), error: null }
    case 'put-wish':
      return { ...state, wishes: upsert(state.wishes, action.wish) }
    case 'drop-wish':
      return { ...state, wishes: state.wishes.filter((w) => w.id !== action.id) }
    case 'put-recipe':
      return { ...state, recipes: upsert(state.recipes, action.recipe) }
    case 'drop-recipe':
      return {
        ...state,
        recipes: state.recipes.filter((r) => r.id !== action.id),
        // The server keeps the items and forgets where they came from.
        shopping: state.shopping.map((it) => (it.recipe_id === action.id ? { ...it, recipe_id: null } : it)),
      }
    case 'put-category':
      return { ...state, categories: sortCategories(upsert(state.categories, action.category)) }
    case 'drop-category':
      return {
        ...state,
        categories: state.categories.filter((c) => c.id !== action.id),
        wishes: state.wishes.map((w) => (w.category_id === action.id ? { ...w, category_id: null } : w)),
      }
    case 'put-tag':
      return { ...state, tags: sortTags(upsert(state.tags, action.tag)) }
    case 'drop-tag':
      return {
        ...state,
        tags: state.tags.filter((t) => t.id !== action.id),
        recipes: state.recipes.map((r) => withoutTag(r, action.id)),
      }
    case 'put-shopping':
      return { ...state, shopping: upsertShopping(state.shopping, action.items) }
    case 'drop-shopping':
      return { ...state, shopping: state.shopping.filter((it) => !action.ids.includes(it.id)) }
  }
}
