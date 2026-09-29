import type { ApiError } from '../../api/errors'
import type { Cook } from '../../api/types'
import { formatDay } from '../../lib/format'
import { ratingOf } from '../../lib/recipes'
import { personSlot, useMe } from '../../state/data'
import { IconStar, IconTrash } from '../../ui/icons'
import { RetryBanner, Section, Skeleton } from '../../ui/layout'
import { Avatar } from '../../ui/media'

interface CookHistoryProps {
  cooks: Cook[] | null
  error: ApiError | null
  onRetry: () => void
  onRate: (cook: Cook) => void
  onDelete: (cook: Cook) => void
}

/** «История»: every time the recipe was cooked, newest first, with each person's stars. */
export function CookHistory({ cooks, error, onRetry, onRate, onDelete }: CookHistoryProps) {
  const me = useMe()
  if (error && !cooks) {
    return (
      <Section header="История">
        <RetryBanner error={error} onRetry={onRetry} />
      </Section>
    )
  }
  if (!cooks) {
    return (
      <Section header="История">
        <div className="timeline timeline--skeleton" aria-busy="true" aria-label="Загружаем историю">
          <Skeleton width="60%" height="16px" radius="8px" />
          <Skeleton width="40%" height="28px" radius="14px" delay={80} />
        </div>
      </Section>
    )
  }
  if (cooks.length === 0) return null

  return (
    <Section header="История">
      <ol className="timeline">
        {cooks.map((cook) => {
          const mine = ratingOf(cook, me.user.id)
          const day = formatDay(cook.cooked_at)
          const comments = cook.ratings.filter((r) => r.comment !== '')
          return (
            <li key={cook.id} className="timeline__item">
              <span className="timeline__rail" aria-hidden="true" />
              <div className="timeline__head">
                <Avatar name={cook.cooked_by.name} slot={personSlot(me, cook.cooked_by.id)} size={24} />
                <span className="timeline__when">
                  <span className="timeline__day">{day}</span>
                  <span className="timeline__who"> · {cook.cooked_by.name}</span>
                </span>
                <button
                  type="button"
                  className="timeline__delete"
                  aria-label={`Удалить запись от ${day}`}
                  onClick={() => onDelete(cook)}
                >
                  <IconTrash size={16} />
                </button>
              </div>
              <div className="timeline__ratings">
                {cook.ratings.map((r) => {
                  const own = r.user.id === me.user.id
                  const chip = (
                    <>
                      <Avatar name={r.user.name} slot={personSlot(me, r.user.id)} size={20} />
                      <IconStar size={14} strokeWidth={2} className="icon-fill rating-chip__star" />
                      <span className="num">{r.stars}</span>
                    </>
                  )
                  return own ? (
                    <button
                      key={r.user.id}
                      type="button"
                      className="rating-chip rating-chip--own"
                      aria-label={`Ваша оценка ${r.stars} из 5 — изменить`}
                      onClick={() => onRate(cook)}
                    >
                      {chip}
                    </button>
                  ) : (
                    <span key={r.user.id} className="rating-chip" role="img" aria-label={`${r.user.name}: ${r.stars} из 5`}>
                      {chip}
                    </span>
                  )
                })}
                {!mine && (
                  <button type="button" className="rating-chip rating-chip--ask" onClick={() => onRate(cook)}>
                    <IconStar size={14} strokeWidth={2.2} />
                    Оценить
                  </button>
                )}
              </div>
              {comments.map((r) => (
                <p key={r.user.id} className="timeline__comment">
                  «{r.comment}»<span className="timeline__comment-by"> — {r.user.name}</span>
                </p>
              ))}
            </li>
          )
        })}
      </ol>
    </Section>
  )
}
