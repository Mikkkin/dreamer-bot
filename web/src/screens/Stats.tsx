import { useEffect, useMemo, useState, type CSSProperties, type ReactNode } from 'react'
import { ApiError } from '../api/errors'
import type { Category, Stats as StatsData, Wish } from '../api/types'
import { categoryBackdrop, categoryEmoji } from '../lib/categoryStyle'
import {
  DREAM_FORMS,
  WISH_FORMS,
  countOf,
  formatMoney,
  parseMinor,
  percent,
  plural,
  sumByCurrency,
  type CurrencyTotal,
} from '../lib/format'
import { everyone, personSlot, useData, useMe } from '../state/data'
import { DonutRing } from '../ui/art'
import { IconCookingPot, IconHeart, IconPiggy, IconSparkles } from '../ui/icons'
import { Cell, EmptyState, IconTile, RetryBanner, Section, Skeleton } from '../ui/layout'
import { Avatar } from '../ui/media'
import { useCountUp } from '../ui/useCountUp'

/** Genitive after «из»: из 1 желания, из 5 желаний. */
const WISH_GENITIVE_FORMS = ['желания', 'желаний', 'желаний'] as const

export function Stats() {
  const data = useData()
  const [stats, setStats] = useState<StatsData | null>(null)
  const [error, setError] = useState<ApiError | null>(null)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    data.api.stats().then(
      (s) => {
        if (!cancelled) setStats(s)
      },
      (err: unknown) => {
        if (!cancelled && err instanceof ApiError && !err.isAuth) setError(err)
      },
    )
    return () => {
      cancelled = true
    }
  }, [data.api, attempt])

  return (
    <div className="page stats">
      <h1 className="page__title">Статистика</h1>
      {error && !stats && (
        <RetryBanner
          error={error}
          onRetry={() => {
            setError(null)
            setAttempt((n) => n + 1)
          }}
        />
      )}
      {stats ? <StatsBody stats={stats} wishes={data.wishes} /> : !error && <StatsSkeleton />}
    </div>
  )
}

function StatsSkeleton() {
  return (
    <div aria-busy="true" aria-label="Загружаем статистику">
      <Skeleton className="stats-hero stats-hero--skeleton" height="144px" />
      <div className="tiles">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="tile" height="96px" delay={80 + i * 80} />
        ))}
      </div>
      <Skeleton className="stats__block" height="120px" radius="16px" delay={320} />
      <Skeleton className="stats__block" height="200px" radius="16px" delay={400} />
    </div>
  )
}

function StatsBody({ stats, wishes }: { stats: StatsData; wishes: Wish[] }) {
  const me = useMe()
  const { overall } = stats
  const total = overall.want.count + overall.progress.count + overall.done.count
  const symbols = useMemo(() => new Map(me.currencies.map((c) => [c.code, c.symbol])), [me.currencies])

  // The server sums per status; "planned" is want + progress, still per currency.
  const planned = useMemo(
    () => sumByCurrency([...overall.want.sums, ...overall.progress.sums]).map((t) => ({ ...t, count: pricedOpen(wishes, t.currency) })),
    [overall, wishes],
  )
  const fulfilled = useMemo(() => sumByCurrency(overall.done.sums), [overall])

  const rows = stats.categories
    .map((c) => ({ category: c.category, count: c.by_status.want.count + c.by_status.progress.count + c.by_status.done.count }))
    .filter((r) => r.count > 0)
    .sort((a, b) => b.count - a.count)
  const categorised = rows.reduce((s, r) => s + r.count, 0)

  const people = everyone(me)
  const byAuthor = people.map((p) => ({ person: p, count: wishes.filter((w) => w.author.id === p.id).length }))
  const authored = byAuthor.reduce((s, a) => s + a.count, 0)

  const extras = (
    <Section>
      {total > 0 && (
        <Cell
          before={
            <IconTile tone="rose">
              <IconSparkles size={18} strokeWidth={2.1} />
            </IconTile>
          }
          title="Сбылось в этом году"
          after={<span className="num stat-count">{stats.fulfilled_this_year}</span>}
        />
      )}
      <Cell
        before={
          <IconTile tone="orange">
            <IconCookingPot size={18} strokeWidth={2.1} />
          </IconTile>
        }
        title="Рецептов"
        after={<span className="num stat-count">{stats.recipes}</span>}
      />
    </Section>
  )

  if (total === 0) {
    return (
      <>
        <EmptyState emoji="📊" title="Статистика появится чуть позже" text="Добавьте пару желаний — и здесь станет интересно." />
        {extras}
      </>
    )
  }

  const pct = percent(overall.done.count, total)

  return (
    <>
      <div className="stats-hero">
        <DonutRing className="stats-hero__ring" value={overall.done.count / total} size={112} stroke={12} label={`Сбылось ${pct}%`}>
          <span className="stats-hero__pct num">
            <CountUp value={pct} />%
          </span>
          <span className="stats-hero__pct-label">сбылось</span>
        </DonutRing>
        <div className="stats-hero__text">
          <p className="stats-hero__of num">
            {overall.done.count} из {total} {plural(total, WISH_GENITIVE_FORMS)}
          </p>
          <p className="stats-hero__fun">{funLine(stats, wishes, rows[0]?.category ?? null)}</p>
          {authored > 0 && (
            <div className="stats-hero__people">
              <div className="people">
                {byAuthor.map((a) => (
                  <span key={a.person.id} className="people__item">
                    <Avatar name={a.person.name} slot={personSlot(me, a.person.id)} />
                    <span className="people__name">{a.person.name}</span>
                    <span className="people__count num">{a.count}</span>
                  </span>
                ))}
              </div>
              {people.length > 1 && (
                <span className="split" aria-hidden="true">
                  {byAuthor.map((a) =>
                    a.count > 0 ? (
                      <span key={a.person.id} className={`split__${personSlot(me, a.person.id)}`} style={{ flexGrow: a.count }} />
                    ) : null,
                  )}
                </span>
              )}
            </div>
          )}
        </div>
      </div>

      <div className="tiles">
        <Tile tone="want" icon={<IconHeart size={16} strokeWidth={2.2} />} value={overall.want.count} label="Хотим" />
        <Tile tone="progress" icon={<IconPiggy size={16} strokeWidth={2.2} />} value={overall.progress.count} label="Копим" />
        <Tile tone="done" icon={<IconSparkles size={16} strokeWidth={2.2} />} value={overall.done.count} label="Сбылось" />
      </div>

      {planned.length > 0 && (
        <Section header="В планах">
          {planned.map((t) => (
            <MoneyRow key={t.currency} total={t} symbol={symbols.get(t.currency) ?? t.currency} note={countOf(t.count, WISH_FORMS)} />
          ))}
        </Section>
      )}

      {fulfilled.length > 0 && (
        <Section header="Сбылось на сумму">
          {fulfilled.map((t) => (
            <MoneyRow key={t.currency} total={t} symbol={symbols.get(t.currency) ?? t.currency} done />
          ))}
        </Section>
      )}

      {rows.length > 0 && (
        <Section header="По категориям">
          <div className="share" aria-hidden="true">
            {rows.map((r, i) => (
              <span
                key={r.category?.id ?? 'none'}
                style={{ '--n': r.count, '--i': i, '--bd-e': categoryBackdrop(r.category).edge } as CSSProperties}
              />
            ))}
          </div>
          <ul className="legend">
            {rows.map((r) => (
              <li key={r.category?.id ?? 'none'} className="legend-row" style={{ '--bd-e': categoryBackdrop(r.category).edge } as CSSProperties}>
                <span className="legend-row__dot" aria-hidden="true" />
                <span className="legend-row__emoji" aria-hidden="true">
                  {categoryEmoji(r.category)}
                </span>
                <span className="legend-row__name">{r.category?.name ?? 'Без категории'}</span>
                <span className="legend-row__count num">{r.count}</span>
                <span className="legend-row__pct num">{percent(r.count, categorised)}%</span>
              </li>
            ))}
          </ul>
        </Section>
      )}

      {extras}
    </>
  )
}

/** A number that counts up on first show. */
function CountUp({ value }: { value: number }) {
  return <>{useCountUp(value)}</>
}

function Tile({ tone, icon, value, label }: { tone: 'want' | 'progress' | 'done'; icon: ReactNode; value: number; label: string }) {
  return (
    <div className={`tile tile--${tone}`}>
      <span className={`tile__icon tile__icon--${tone}`} aria-hidden="true">
        {icon}
      </span>
      <span className="tile__value num">{value}</span>
      <span className="tile__label">{label}</span>
    </div>
  )
}

function MoneyRow({ total, symbol, note, done }: { total: CurrencyTotal; symbol: string; note?: string; done?: boolean }) {
  return (
    <div className="stat-row">
      <span className={done ? 'money-tile money-tile--done' : 'money-tile'} aria-hidden="true">
        {symbol}
      </span>
      <span className="num stat-row__money">{formatMoney(total.minor, total.currency)}</span>
      {note && <span className="stat-row__note">{note}</span>}
    </div>
  )
}

function pricedOpen(wishes: Wish[], currency: string): number {
  return wishes.filter((w) => w.status !== 'done' && w.price?.currency === currency).length
}

/** One warm line picked from the data. */
function funLine(stats: StatsData, wishes: Wish[], topCategory: Category | null): string {
  const done = stats.overall.done.count
  if (done > 0 && done % 2 === 1) return `Вы уже исполнили ${done} ${plural(done, DREAM_FORMS)} вместе 💞`
  const ambitious = mostAmbitious(wishes)
  if (ambitious?.price) return `Самая амбициозная мечта — «${ambitious.title}» (${ambitious.price.formatted}) 🚀`
  if (topCategory) return `Больше всего мечтаете о: ${topCategory.emoji} ${topCategory.name}`
  if (done > 0) return `Вы уже исполнили ${done} ${plural(done, DREAM_FORMS)} вместе 💞`
  return 'Первая сбывшаяся мечта — уже совсем скоро ✨'
}

/** The priciest open wish in the most common currency (currencies are never compared). */
function mostAmbitious(wishes: Wish[]): Wish | null {
  const priced = wishes.flatMap((w) => (w.status !== 'done' && w.price ? [{ wish: w, price: w.price }] : []))
  const currency = sumByCurrency(priced.map((p) => p.price)).sort((a, b) => b.count - a.count)[0]?.currency
  let best: Wish | null = null
  let bestMinor = -1
  for (const { wish, price } of priced) {
    const minor = price.currency === currency ? (parseMinor(price.amount) ?? -1) : -1
    if (minor > bestMinor) {
      best = wish
      bestMinor = minor
    }
  }
  return best
}
