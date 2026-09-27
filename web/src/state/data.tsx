import { createContext, use, useCallback, useEffect, useMemo, useReducer, useRef, useState, type ReactNode } from 'react'
import { ApiClient } from '../api/client'
import { ApiError } from '../api/errors'
import type { Category, Me, Person, Recipe, Wish } from '../api/types'

// The whole dataset of a couple is small, so it is loaded once and filtered on
// the client. Mutations update the local copy right away and then refetch.

export type Fatal = 'unauthorized' | 'forbidden'
export type Collection = 'wishes' | 'recipes' | 'categories'

interface State {
  phase: 'loading' | 'ready' | 'failed'
  me: Me | null
  wishes: Wish[]
  recipes: Recipe[]
  categories: Category[]
  /** A failed load or refresh that the user can retry from a banner. */
  error: ApiError | null
}

type Action =
  | { type: 'loading' }
  | { type: 'loaded'; me: Me; wishes: Wish[]; recipes: Recipe[]; categories: Category[] }
  | { type: 'failed'; error: ApiError }
  | { type: 'wishes'; wishes: Wish[] }
  | { type: 'recipes'; recipes: Recipe[] }
  | { type: 'categories'; categories: Category[] }
  | { type: 'put-wish'; wish: Wish }
  | { type: 'drop-wish'; id: number }
  | { type: 'put-recipe'; recipe: Recipe }
  | { type: 'drop-recipe'; id: number }
  | { type: 'put-category'; category: Category }
  | { type: 'drop-category'; id: number }

const initialState: State = { phase: 'loading', me: null, wishes: [], recipes: [], categories: [], error: null }

function upsert<T extends { id: number }>(list: T[], item: T): T[] {
  const i = list.findIndex((x) => x.id === item.id)
  return i < 0 ? [item, ...list] : list.map((x) => (x.id === item.id ? item : x))
}

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'loading':
      return { ...state, phase: state.phase === 'ready' ? 'ready' : 'loading' }
    case 'loaded':
      return {
        phase: 'ready',
        me: action.me,
        wishes: action.wishes,
        recipes: action.recipes,
        categories: sortCategories(action.categories),
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
    case 'put-wish':
      return { ...state, wishes: upsert(state.wishes, action.wish) }
    case 'drop-wish':
      return { ...state, wishes: state.wishes.filter((w) => w.id !== action.id) }
    case 'put-recipe':
      return { ...state, recipes: upsert(state.recipes, action.recipe) }
    case 'drop-recipe':
      return { ...state, recipes: state.recipes.filter((r) => r.id !== action.id) }
    case 'put-category':
      return { ...state, categories: sortCategories(upsert(state.categories, action.category)) }
    case 'drop-category':
      return {
        ...state,
        categories: state.categories.filter((c) => c.id !== action.id),
        wishes: state.wishes.map((w) => (w.category_id === action.id ? { ...w, category_id: null } : w)),
      }
  }
}

function sortCategories(list: Category[]): Category[] {
  return [...list].sort((a, b) => a.position - b.position || a.id - b.id)
}

export interface Data extends State {
  api: ApiClient
  reload(): Promise<void>
  refresh(...collections: Collection[]): Promise<void>
  putWish(wish: Wish): void
  dropWish(id: number): void
  putRecipe(recipe: Recipe): void
  dropRecipe(id: number): void
  putCategory(category: Category): void
  dropCategory(id: number): void
}

const DataContext = createContext<Data | null>(null)

export function useData(): Data {
  const data = use(DataContext)
  if (!data) throw new Error('useData must be used inside <DataProvider>')
  return data
}

/** The loaded profile; only valid below the loading gate. */
export function useMe(): Me {
  const { me } = useData()
  if (!me) throw new Error('useMe called before /api/me loaded')
  return me
}

const STALE_AFTER_MS = 60_000

interface DataProviderProps {
  initData: string
  onFatal: (fatal: Fatal) => void
  /** Subscribes to "the app is visible again"; returns the unsubscribe. */
  watchActivation: (handler: () => void) => () => void
  children: ReactNode
}

export function DataProvider({ initData, onFatal, watchActivation, children }: DataProviderProps) {
  const [state, dispatch] = useReducer(reducer, initialState)
  const [api] = useState(
    () => new ApiClient(initData, (e) => onFatal(e.status === 403 ? 'forbidden' : 'unauthorized')),
  )
  const loadedAt = useRef(0)

  const reload = useCallback(async () => {
    dispatch({ type: 'loading' })
    try {
      const [me, wishes, recipes, categories] = await Promise.all([api.me(), api.wishes(), api.recipes(), api.categories()])
      loadedAt.current = Date.now()
      dispatch({ type: 'loaded', me, wishes, recipes, categories })
    } catch (err) {
      if (err instanceof ApiError && !err.isAuth) dispatch({ type: 'failed', error: err })
    }
  }, [api])

  const refresh = useCallback(
    async (...collections: Collection[]) => {
      try {
        await Promise.all(
          collections.map(async (c) => {
            if (c === 'wishes') dispatch({ type: 'wishes', wishes: await api.wishes() })
            else if (c === 'recipes') dispatch({ type: 'recipes', recipes: await api.recipes() })
            else dispatch({ type: 'categories', categories: await api.categories() })
          }),
        )
      } catch (err) {
        if (err instanceof ApiError && !err.isAuth) dispatch({ type: 'failed', error: err })
      }
    },
    [api],
  )

  useEffect(() => {
    void reload()
  }, [reload])

  // When the app comes back from the background, pick up what the partner added.
  useEffect(
    () =>
      watchActivation(() => {
        if (loadedAt.current > 0 && Date.now() - loadedAt.current > STALE_AFTER_MS) void reload()
      }),
    [watchActivation, reload],
  )

  const value = useMemo<Data>(
    () => ({
      ...state,
      api,
      reload,
      refresh,
      putWish: (wish) => dispatch({ type: 'put-wish', wish }),
      dropWish: (id) => dispatch({ type: 'drop-wish', id }),
      putRecipe: (recipe) => dispatch({ type: 'put-recipe', recipe }),
      dropRecipe: (id) => dispatch({ type: 'drop-recipe', id }),
      putCategory: (category) => dispatch({ type: 'put-category', category }),
      dropCategory: (id) => dispatch({ type: 'drop-category', id }),
    }),
    [state, api, reload, refresh],
  )

  return <DataContext value={value}>{children}</DataContext>
}

/** Stable colour slot per person: the lower Telegram id is "A" on both partners' phones. */
export function personSlot(me: Me, personId: number): 'a' | 'b' {
  const ids = [me.user.id, ...me.partners.map((p) => p.id)].sort((x, y) => x - y)
  return ids.indexOf(personId) === 0 ? 'a' : 'b'
}

export function everyone(me: Me): Person[] {
  return [me.user, ...me.partners].sort((x, y) => x.id - y.id)
}
