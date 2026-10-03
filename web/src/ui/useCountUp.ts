import { useEffect, useRef, useState } from 'react'

/**
 * Counts up to `target`, easing out: from 0 the first time, then from the
 * number on screen when the target changes (a stepper tap never restarts
 * from 0). Instant under reduced motion or on low-end devices.
 */
export function useCountUp(target: number, durationMs = 600): number {
  const [value, setValue] = useState(() => (skipMotion() ? target : 0))
  const shown = useRef(value)

  useEffect(() => {
    if (skipMotion()) {
      shown.current = target
      setValue(target)
      return
    }
    const from = shown.current
    let frame = 0
    const start = performance.now()
    const tick = (now: number) => {
      const k = Math.min(1, (now - start) / durationMs)
      const v = Math.round(from + (target - from) * (1 - (1 - k) ** 3))
      shown.current = v
      setValue(v)
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
