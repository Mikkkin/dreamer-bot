import { createContext, use, useEffect, useEffectEvent, useLayoutEffect, useMemo, useReducer, useRef, type ReactNode } from 'react'
import type { ApiImage } from '../api/types'
import { useTelegram } from '../telegram/hooks'
import { ScreenContext } from './screen'

// In-memory navigation stack synced with Telegram's BackButton. There is no URL
// router: the hash belongs to Telegram's launch parameters.

export type Section = 'wishes' | 'recipes'

export type Route =
  | { name: 'home'; section: Section }
  | { name: 'wish'; id: number }
  | { name: 'wish-form'; id?: number; categoryId?: number | null }
  | { name: 'recipe'; id: number; random?: boolean }
  /** imported: the recipe was just imported and saved; the form asks to check it. */
  | { name: 'recipe-form'; id?: number; imported?: { warnings: string[] } }
  | { name: 'viewer'; images: ApiImage[]; start: number; title: string }
  | { name: 'stats' }
  | { name: 'categories' }
  | { name: 'recipe-tags' }
  | { name: 'shopping' }

interface Entry {
  key: number
  route: Route
}

interface State {
  entries: Entry[]
  nextKey: number
  direction: 'forward' | 'back'
}

type Action = { type: 'push'; route: Route } | { type: 'replace'; route: Route } | { type: 'pop' }

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'push':
      return {
        entries: [...state.entries, { key: state.nextKey, route: action.route }],
        nextKey: state.nextKey + 1,
        direction: 'forward',
      }
    case 'replace':
      return {
        entries: [...state.entries.slice(0, -1), { key: state.nextKey, route: action.route }],
        nextKey: state.nextKey + 1,
        direction: 'forward',
      }
    case 'pop':
      return state.entries.length > 1 ? { ...state, entries: state.entries.slice(0, -1), direction: 'back' } : state
  }
}

export interface Nav {
  push(route: Route): void
  replace(route: Route): void
  pop(): void
  top: Route
  depth: number
}

const NavContext = createContext<Nav | null>(null)

export function useNav(): Nav {
  const nav = use(NavContext)
  if (!nav) throw new Error('useNav must be used inside <Navigator>')
  return nav
}

interface NavigatorProps {
  initial: Route[]
  renderRoute: (route: Route) => ReactNode
  children?: ReactNode
}

export function Navigator({ initial, renderRoute, children }: NavigatorProps) {
  const tg = useTelegram()
  const [state, dispatch] = useReducer(reducer, initial, (routes): State => ({
    entries: routes.map((route, key) => ({ key, route })),
    nextKey: routes.length,
    direction: 'forward',
  }))
  const scrolls = useRef(new Map<number, number>())
  const topEntry = state.entries.at(-1)
  if (!topEntry) throw new Error('navigation stack is empty')
  const depth = state.entries.length

  const nav = useMemo<Nav>(
    () => ({
      push: (route) => {
        scrolls.current.set(topEntry.key, window.scrollY)
        dispatch({ type: 'push', route })
      },
      replace: (route) => dispatch({ type: 'replace', route }),
      pop: () => dispatch({ type: 'pop' }),
      top: topEntry.route,
      depth,
    }),
    [topEntry, depth],
  )

  // Screens below the top stay mounted (hidden), so restoring the saved offset
  // brings the user back exactly where they were.
  useLayoutEffect(() => {
    const y = state.direction === 'back' ? (scrolls.current.get(topEntry.key) ?? 0) : 0
    window.scrollTo(0, y)
    for (const key of scrolls.current.keys()) {
      if (!state.entries.some((e) => e.key === key)) scrolls.current.delete(key)
    }
  }, [topEntry.key, state.direction, state.entries])

  useEffect(() => {
    tg.back.setBase(() => dispatch({ type: 'pop' }), depth > 1)
  }, [tg, depth])

  const openCategories = useEffectEvent(() => {
    if (topEntry.route.name !== 'categories') nav.push({ name: 'categories' })
  })
  useEffect(() => tg.onSettings(() => openCategories()), [tg])

  return (
    <NavContext value={nav}>
      {state.entries.map((entry) => {
        const isTop = entry === topEntry
        return (
          <ScreenContext key={entry.key} value={isTop ? TOP : BELOW}>
            <div className="screen" hidden={!isTop} data-dir={isTop ? state.direction : undefined}>
              {renderRoute(entry.route)}
            </div>
          </ScreenContext>
        )
      })}
      {children}
    </NavContext>
  )
}

const TOP = { isTop: true }
const BELOW = { isTop: false }
