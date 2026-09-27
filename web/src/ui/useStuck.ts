import { useEffect, useState } from 'react'

/**
 * Tells whether a sticky bar is currently stuck to the top: a 1 px sentinel
 * placed right before the bar has scrolled out above the viewport. Attach
 * `sentinel` as the sentinel's ref (it may mount later than the screen).
 */
export function useStuck() {
  const [el, sentinel] = useState<HTMLDivElement | null>(null)
  const [stuck, setStuck] = useState(false)

  useEffect(() => {
    if (!el || typeof IntersectionObserver === 'undefined') return
    const io = new IntersectionObserver(([entry]) => {
      if (entry) setStuck(!entry.isIntersecting && entry.boundingClientRect.top < 0)
    })
    io.observe(el)
    return () => io.disconnect()
  }, [el])

  return { sentinel, stuck }
}
