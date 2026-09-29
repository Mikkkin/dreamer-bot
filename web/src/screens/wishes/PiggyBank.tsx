import { useEffect, useState } from 'react'
import { ApiError } from '../../api/errors'
import type { Saving, Wish } from '../../api/types'
import { formatMoney, formatShortDay } from '../../lib/format'
import { savingsProgress } from '../../lib/savings'
import { personSlot, useData, useMe } from '../../state/data'
import { DonutRing } from '../../ui/art'
import { IconPiggy, IconPlus, IconSparkles, IconTrash } from '../../ui/icons'
import { Cell, IconTile, RetryBanner, Section, Skeleton } from '../../ui/layout'
import { Avatar } from '../../ui/media'
import { useCountUp } from '../../ui/useCountUp'

/**
 * The wish's contributions, refetched whenever its saved total changes. A wish
 * taken from a list has `saved.count: null`, so `saved` itself is the signal.
 */
export function useSavings(wish: Wish | null) {
  const data = useData()
  const [state, setState] = useState<{ key: string; list: Saving[] | null; error: ApiError | null } | null>(null)
  const [version, setVersion] = useState(0)
  const id = wish?.id ?? 0
  const hasSavings = wish?.saved != null
  const signature = `${id}:${wish?.saved?.count ?? ''}:${wish?.saved?.total.amount ?? ''}:${version}`

  useEffect(() => {
    if (id === 0 || !hasSavings) {
      // The savings emptied: a later contribution must not bring the old list back.
      setState(null)
      return
    }
    let cancelled = false
    data.api.savings(id).then(
      (list) => {
        if (!cancelled) setState({ key: signature, list, error: null })
      },
      (err: unknown) => {
        if (cancelled || !(err instanceof ApiError) || err.isAuth) return
        setState((s) => ({ key: signature, list: s?.list ?? null, error: err }))
      },
    )
    return () => {
      cancelled = true
    }
  }, [data.api, id, hasSavings, signature])

  const current = state && state.key.startsWith(`${id}:`) ? state : null
  return {
    savings: hasSavings ? (current?.list ?? null) : [],
    error: current?.key === signature ? current.error : null,
    reload: () => setVersion((v) => v + 1),
  }
}

interface PiggyBankProps {
  wish: Wish
  savings: Saving[] | null
  error: ApiError | null
  onRetry: () => void
  onAdd: () => void
  onRemove: (saving: Saving) => void
  onFulfil: () => void
  busy: boolean
}

/** «Копилка»: how much is put aside, every contribution, and «＋ Отложить». */
export function PiggyBank({ wish, savings, error, onRetry, onAdd, onRemove, onFulfil, busy }: PiggyBankProps) {
  const me = useMe()
  const progress = savingsProgress(wish)
  const done = wish.status === 'done'
  const pct = progress?.percent ?? 0
  const shown = useCountUp(pct)

  return (
    <Section header="Копилка">
      {progress ? (
        <div className="piggy">
          {progress.priceMinor !== null ? (
            <DonutRing className="piggy__ring" value={progress.savedMinor / progress.priceMinor} size={84} stroke={9} label={`Накоплено ${pct}%`}>
              <span className="piggy__pct num">{shown}%</span>
            </DonutRing>
          ) : (
            <span className="piggy__icon" aria-hidden="true">
              <IconPiggy size={34} strokeWidth={1.8} />
            </span>
          )}
          <div className="piggy__text">
            <span className="piggy__label">{done ? 'Было отложено' : 'Накоплено'}</span>
            <span className="piggy__total num">{formatMoney(progress.savedMinor, progress.currency)}</span>
            <span className="piggy__of num">
              {progress.priceMinor !== null
                ? progress.complete
                  ? 'вся сумма собрана'
                  : `из ${formatMoney(progress.priceMinor, progress.currency)} · осталось ${formatMoney(progress.priceMinor - progress.savedMinor, progress.currency)}`
                : 'Укажите сумму мечты — покажем прогресс'}
            </span>
          </div>
        </div>
      ) : (
        <p className="piggy__empty">Откладывайте понемногу — здесь будет видно, сколько уже собрано.</p>
      )}

      {progress?.complete && !done && (
        <div className="piggy-banner" role="status">
          <span className="piggy-banner__text">Всё накоплено! Отметить «Сбылось»?</span>
          <button type="button" className="piggy-banner__action" onClick={onFulfil} disabled={busy}>
            <IconSparkles size={16} strokeWidth={2.2} />
            Сбылось
          </button>
        </div>
      )}

      {error && <RetryBanner error={error} onRetry={onRetry} />}
      {!savings && !error && wish.saved && (
        <div className="savings-list savings-list--skeleton" aria-busy="true">
          <Skeleton height="44px" radius="12px" />
        </div>
      )}
      {savings && savings.length > 0 && (
        <ul className="savings-list">
          {savings.map((s) => (
            <li key={s.id} className="saving-row">
              <Avatar name={s.user.name} slot={personSlot(me, s.user.id)} size={28} />
              <span className="saving-row__main">
                <span className="saving-row__amount num">+{s.amount.formatted}</span>
                <span className="saving-row__meta">
                  {[s.user.name, formatShortDay(s.created_at), s.note].filter(Boolean).join(' · ')}
                </span>
              </span>
              <button
                type="button"
                className="saving-row__delete"
                aria-label={`Удалить взнос ${s.amount.formatted}`}
                onClick={() => onRemove(s)}
              >
                <IconTrash size={16} />
              </button>
            </li>
          ))}
        </ul>
      )}

      {!done && (
        <Cell
          before={
            <IconTile tone="amber">
              <IconPlus size={18} strokeWidth={2.4} />
            </IconTile>
          }
          title="Отложить"
          tone="accent"
          onClick={onAdd}
        />
      )}
    </Section>
  )
}
