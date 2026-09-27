import type { EntityLookup } from './useEntity'
import { EmptyState, RetryBanner, Skeleton } from '../../ui/layout'

/** What a detail screen shows while its entity is not available. */
export function LoadState<T>({ lookup, missingTitle, missingText }: { lookup: EntityLookup<T>; missingTitle: string; missingText: string }) {
  if (lookup.missing) return <EmptyState emoji="🕊" title={missingTitle} text={missingText} />
  if (lookup.error) return <RetryBanner error={lookup.error} onRetry={lookup.retry} />
  return (
    <div className="detail-skeleton" aria-busy="true">
      <Skeleton className="detail-skeleton__media" />
      <Skeleton className="detail-skeleton__line detail-skeleton__line--overline" delay={80} />
      <Skeleton className="detail-skeleton__line" delay={120} />
      <Skeleton className="detail-skeleton__line detail-skeleton__line--short" delay={160} />
    </div>
  )
}
