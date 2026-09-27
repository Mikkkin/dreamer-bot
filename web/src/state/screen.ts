import { createContext, use, useCallback, useEffect, useRef } from 'react'

/** Whether the screen that renders a component is the visible top of the navigation stack. */
export const ScreenContext = createContext<{ isTop: boolean }>({ isTop: true })

export function useIsTopScreen(): boolean {
  return use(ScreenContext).isTop
}

/**
 * For async handlers: tells whether the screen is still mounted and on top
 * after an await, so a late response never pops or replaces someone else's screen.
 */
export function useStillOnTop(): () => boolean {
  const isTop = useIsTopScreen()
  const ref = useRef(isTop)
  useEffect(() => {
    ref.current = isTop
    return () => {
      ref.current = false
    }
  }, [isTop])
  return useCallback(() => ref.current, [])
}
