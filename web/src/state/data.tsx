import { createContext, use, useCallback, useEffect, useMemo, useReducer, useRef, useState, type ReactNode } from 'react'
import { ApiClient } from '../api/client'
import { ApiError } from '../api/errors'
import type { Category, Me, Person, Recipe, RecipeTag, ShoppingItem, Store, Wish } from '../api/types'
import { initialState, reducer, type State } from './store'

// The whole dataset of a couple is small, so it is loaded once and filtered on
// the client. Mutations update the local copy right away and then refetch.

export type Fatal = 'unauthorized' | 'forbidden'
export type Collection = 'wishes' | 'recipes' | 'categories' | 'tags' | 'shopping'

export interface Data extends State {
  api: ApiClient
  reload(): Promise<void>
  refresh(...collections: Collection[]): Promise<void>
  /** Refetches one wish (its savings total or status changed on the server); false when that failed. */
  refreshWish(id: number): Promise<boolean>
  /** Refetches one recipe (its cooking summary changed on the server). */
  refreshRecipe(id: number): Promise<void>
  /** Loads the store links once; later calls reuse them. */
  loadStores(): Promise<void>
  putWish(wish: Wish): void
  dropWish(id: number): void
  putRecipe(recipe: Recipe): void
  dropRecipe(id: number): void
  putCategory(category: Category): void
  dropCategory(id: number): void
  putTag(tag: RecipeTag): void
  dropTag(id: number): void
  putShopping(items: ShoppingItem[]): void
  dropShopping(ids: number[]): void
  setShopping(items: ShoppingItem[]): void
  setStores(stores: Store[]): void
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
  const storesLoaded = useRef(false)

  const reload = useCallback(async () => {
    dispatch({ type: 'loading' })
    try {
      const [me, wishes, recipes, categories, tags, shopping] = await Promise.all([
        api.me(),
        api.wishes(),
        api.recipes(),
        api.categories(),
        api.recipeTags(),
        api.shopping(),
      ])
      loadedAt.current = Date.now()
      dispatch({ type: 'loaded', me, wishes, recipes, categories, tags, shopping })
    } catch (err) {
      if (err instanceof ApiError && !err.isAuth) dispatch({ type: 'failed', error: err })
    }
  }, [api])

  const refresh = useCallback(
    async (...collections: Collection[]) => {
      try {
        await Promise.all(
          collections.map(async (c) => {
            switch (c) {
              case 'wishes':
                return dispatch({ type: 'wishes', wishes: await api.wishes() })
              case 'recipes':
                return dispatch({ type: 'recipes', recipes: await api.recipes() })
              case 'categories':
                return dispatch({ type: 'categories', categories: await api.categories() })
              case 'tags':
                return dispatch({ type: 'tags', tags: await api.recipeTags() })
              case 'shopping':
                return dispatch({ type: 'shopping', items: await api.shopping() })
            }
          }),
        )
      } catch (err) {
        if (err instanceof ApiError && !err.isAuth) dispatch({ type: 'failed', error: err })
      }
    },
    [api],
  )

  const refreshWish = useCallback(
    async (id: number) => {
      try {
        dispatch({ type: 'put-wish', wish: await api.wish(id) })
        return true
      } catch (err) {
        if (err instanceof ApiError && err.code === 'not_found') {
          dispatch({ type: 'drop-wish', id })
          return true
        }
        return false
      }
    },
    [api],
  )

  const refreshRecipe = useCallback(
    async (id: number) => {
      try {
        dispatch({ type: 'put-recipe', recipe: await api.recipe(id) })
      } catch (err) {
        if (err instanceof ApiError && err.code === 'not_found') dispatch({ type: 'drop-recipe', id })
      }
    },
    [api],
  )

  const loadStores = useCallback(async () => {
    if (storesLoaded.current) return
    storesLoaded.current = true
    try {
      dispatch({ type: 'stores', stores: await api.stores() })
    } catch {
      // Store links are a convenience: without them the list still works.
      storesLoaded.current = false
    }
  }, [api])

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
      refreshWish,
      refreshRecipe,
      loadStores,
      putWish: (wish) => dispatch({ type: 'put-wish', wish }),
      dropWish: (id) => dispatch({ type: 'drop-wish', id }),
      putRecipe: (recipe) => dispatch({ type: 'put-recipe', recipe }),
      dropRecipe: (id) => dispatch({ type: 'drop-recipe', id }),
      putCategory: (category) => dispatch({ type: 'put-category', category }),
      dropCategory: (id) => dispatch({ type: 'drop-category', id }),
      putTag: (tag) => dispatch({ type: 'put-tag', tag }),
      dropTag: (id) => dispatch({ type: 'drop-tag', id }),
      putShopping: (items) => dispatch({ type: 'put-shopping', items }),
      dropShopping: (ids) => dispatch({ type: 'drop-shopping', ids }),
      setShopping: (items) => dispatch({ type: 'shopping', items }),
      setStores: (stores) => dispatch({ type: 'stores', stores }),
    }),
    [state, api, reload, refresh, refreshWish, refreshRecipe, loadStores],
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
