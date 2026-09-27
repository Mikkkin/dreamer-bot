import { useEffect, useState } from 'react'

/** Counts from 0 up to `target` once, easing out; instant under reduced motion or on low-end devices. */
export function useCountUp(target: number, durationMs = 600): number {
  const [value, setValue] = useState(() => (skipMotion() ? target : 0))

  useEffect(() => {
    if (skipMotion()) {
      setValue(target)
      return
    }
    let frame = 0
    const start = performance.now()
    const tick = (now: number) => {
      const k = Math.min(1, (now - start) / durationMs)
      setValue(Math.round(target * (1 - (1 - k) ** 3)))
      if (k < 1) frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [target, durationMs])

  return value
}

function skipMotion(): boolean {
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches || document.documentElement.dataset.perf === 'low'
}
