import { useEffect, useEffectEvent, useState } from 'react'
import { ApiError } from '../../api/errors'

export interface EntityLookup<T> {
  entity: T | null
  /** The server says it does not exist (deleted meanwhile, or a stale deep link). */
  missing: boolean
  error: ApiError | null
  retry: () => void
}

/**
 * Resolves an item from the loaded list, falling back to a single GET when it
 * is not there yet (a deep link racing a partner's fresh addition).
 */
export function useEntity<T extends { id: number }>(
  list: readonly T[],
  id: number | null,
  fetchOne: (id: number) => Promise<T>,
): EntityLookup<T> {
  const fromList = id === null ? null : (list.find((x) => x.id === id) ?? null)
  const [result, setResult] = useState<{ id: number; entity: T | null; error: ApiError | null } | null>(null)
  const [attempt, setAttempt] = useState(0)
  const load = useEffectEvent(fetchOne)
  const inList = fromList !== null

  useEffect(() => {
    if (inList || id === null) return
    let cancelled = false
    load(id).then(
      (entity) => {
        if (!cancelled) setResult({ id, entity, error: null })
      },
      (err: unknown) => {
        if (cancelled) return
        const error = err instanceof ApiError ? err : new ApiError(0, 'network', 'Нет соединения')
        setResult({ id, entity: null, error })
      },
    )
    return () => {
      cancelled = true
    }
  }, [id, inList, attempt])

  const own = result?.id === id ? result : null
  return {
    entity: fromList ?? own?.entity ?? null,
    missing: !fromList && own?.error?.code === 'not_found',
    error: !fromList && own?.error && own.error.code !== 'not_found' ? own.error : null,
    retry: () => setAttempt((n) => n + 1),
  }
}
