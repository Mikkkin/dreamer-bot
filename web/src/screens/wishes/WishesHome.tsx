import { useMemo, useState } from 'react'
import type { Category, Status } from '../../api/types'
import { WISH_FORMS, countOf, formatMoney, percent, sumByCurrency } from '../../lib/format'
import { openSavings } from '../../lib/savings'
import { personSlot, useData, useMe } from '../../state/data'
import { useNav } from '../../state/nav'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { DonutRing } from '../../ui/art'
import { Chip, ChipGroup, Segmented } from '../../ui/controls'
import { IconHeart, IconPiggy, IconPlus, IconSparkles } from '../../ui/icons'
import { EmptyState } from '../../ui/layout'
import { useStuck } from '../../ui/useStuck'
import { WishCard } from './WishCard'
import {
  categoryMap,
  categoryOf,
  countByCategory,
  countByStatus,
  inCategory,
  sortWishes,
  type CategoryFilter,
} from './model'

const STATUS_EMPTY: Record<Status, { emoji: string; title: string; text: string }> = {
  want: {
    emoji: '🌱',
    title: 'Здесь пока пусто',
    text: 'Запишите первую общую мечту — маленькую или огромную. Вдвоём мечтать веселее.',
  },
  progress: {
    emoji: '🐷',
    title: 'Пока ни на что не копим',
    text: 'Отметьте желание статусом «Копим», когда начнёте откладывать.',
  },
  done: {
    emoji: '✨',
    title: 'Скоро здесь будет магия',
    text: 'Когда мечта исполнится — отметьте её, и мы вместе порадуемся 🎉',
  },
}

const STATUS_TABS = [
  { value: 'want', label: 'Хотим', tone: 'want', icon: <IconHeart size={16} strokeWidth={2.2} /> },
  { value: 'progress', label: 'Копим', tone: 'progress', icon: <IconPiggy size={16} strokeWidth={2.2} /> },
  { value: 'done', label: 'Сбылось', tone: 'done', icon: <IconSparkles size={16} strokeWidth={2.2} /> },
] as const

function categoryEmpty(category: Category): { emoji: string; title: string; text: string } {
  const text = category.emoji.startsWith('✈') ? 'Куда бы вы хотели поехать вдвоём?' : 'Добавьте сюда первую мечту.'
  return { emoji: category.emoji, title: `В категории «${category.name}» пока пусто`, text }
}

export function WishesHome({ active }: { active: boolean }) {
  const { wishes, categories } = useData()
  const me = useMe()
  const nav = useNav()
  const tg = useTelegram()
  const [status, setStatus] = useState<Status>('want')
  const [rawFilter, setFilter] = useState<CategoryFilter>('all')
  const { sentinel, stuck } = useStuck()

  const byId = useMemo(() => categoryMap(categories), [categories])
  // A category deleted elsewhere falls back to "all".
  const filter: CategoryFilter = typeof rawFilter === 'number' && !byId.has(rawFilter) ? 'all' : rawFilter

  const statusCounts = useMemo(() => countByStatus(wishes.filter((w) => inCategory(w, filter))), [wishes, filter])
  const inStatus = useMemo(() => wishes.filter((w) => w.status === status), [wishes, status])
  const chipCounts = useMemo(() => countByCategory(inStatus), [inStatus])
  const visible = useMemo(() => sortWishes(inStatus.filter((w) => inCategory(w, filter)), status), [inStatus, filter, status])
  const planned = useMemo(
    () => sumByCurrency(wishes.flatMap((w) => (w.status !== 'done' && w.price ? [w.price] : []))),
    [wishes],
  )
  const saved = useMemo(() => openSavings(wishes), [wishes])
  const doneTotal = wishes.filter((w) => w.status === 'done').length
  const uncategorised = chipCounts.get(null) ?? 0

  useMainButton(
    active
      ? {
          text: 'Добавить мечту',
          shine: wishes.length === 0,
          onClick: () => nav.push({ name: 'wish-form', categoryId: typeof filter === 'number' ? filter : null }),
        }
      : null,
  )

  const selectedCategory = typeof filter === 'number' ? byId.get(filter) : undefined
  const empty = selectedCategory && status !== 'done' ? categoryEmpty(selectedCategory) : STATUS_EMPTY[status]

  return (
    <>
      {wishes.length > 0 && (
        <HeroCard
          total={wishes.length}
          done={doneTotal}
          planned={planned.map((t) => formatMoney(t.minor, t.currency))}
          saved={saved.map((t) => formatMoney(t.minor, t.currency))}
          onOpen={() => {
            tg.haptic.impact('light')
            nav.push({ name: 'stats' })
          }}
        />
      )}

      <div className="sticky-sentinel" ref={sentinel} aria-hidden="true" />
      <div className="sticky-bar" data-stuck={stuck || undefined}>
        <div className="sticky-bar__seg">
          <Segmented
            label="Статус"
            value={status}
            onChange={setStatus}
            options={STATUS_TABS.map((s) => ({ ...s, count: statusCounts[s.value] }))}
          />
        </div>
        <ChipGroup label="Категория">
          <Chip selected={filter === 'all'} count={inStatus.length} onSelect={() => setFilter('all')}>
            Все
          </Chip>
          {categories.map((c) => (
            <Chip
              key={c.id}
              selected={filter === c.id}
              emoji={c.emoji}
              count={chipCounts.get(c.id) ?? 0}
              onSelect={() => setFilter(c.id)}
            >
              {c.name}
            </Chip>
          ))}
          {(uncategorised > 0 || filter === 'none') && (
            <Chip selected={filter === 'none'} count={uncategorised} onSelect={() => setFilter('none')}>
              Без категории
            </Chip>
          )}
          <Chip action iconOnly label="Категории" onSelect={() => nav.push({ name: 'categories' })}>
            <IconPlus size={18} strokeWidth={2.2} />
          </Chip>
        </ChipGroup>
      </div>

      {visible.length === 0 ? (
        <EmptyState emoji={empty.emoji} title={empty.title} text={empty.text} />
      ) : (
        <div className="grid" key={`${status}:${filter}`}>
          {visible.map((w, i) => (
            <WishCard
              key={w.id}
              wish={w}
              index={i}
              category={categoryOf(w, byId)}
              showCategory={filter === 'all'}
              authorSlot={personSlot(me, w.author.id)}
              onOpen={() => {
                tg.haptic.impact('light')
                nav.push({ name: 'wish', id: w.id })
              }}
            />
          ))}
        </div>
      )}
    </>
  )
}

/** The emotional summary: what we are dreaming of, and how much has come true. Opens the stats. */
function HeroCard({
  total,
  done,
  planned,
  saved,
  onOpen,
}: {
  total: number
  done: number
  planned: string[]
  saved: string[]
  onOpen: () => void
}) {
  const pct = percent(done, total)
  const hasMoney = planned.length > 0
  const money = planned.join(' · ')
  const label = hasMoney
    ? `Статистика: в планах ${planned.join(' и ')}, сбылось ${done} из ${total}`
    : `Статистика: ${countOf(total, WISH_FORMS)}, сбылось ${done}`
  const savedLine = saved.join(' · ')
  return (
    <button
      type="button"
      className={saved.length > 0 ? 'hero-card hero-card--saved' : 'hero-card'}
      onClick={onOpen}
      aria-label={saved.length > 0 ? `${label}, отложено ${saved.join(' и ')}` : label}
    >
      <span className="hero-card__label">{hasMoney ? 'В планах' : 'Наши мечты'}</span>
      <span className="hero-card__value num">{hasMoney ? money : countOf(total, WISH_FORMS)}</span>
      <span className="hero-card__sub">
        {hasMoney
          ? `${countOf(total, WISH_FORMS)} · сбылось ${done}`
          : done > 0
            ? `Сбылось уже ${done} ✨`
            : 'Первая мечта — уже скоро ✨'}
      </span>
      {saved.length > 0 && (
        <span className="hero-card__saved">
          <IconPiggy size={14} strokeWidth={2.2} />
          <span className="num">Отложено {savedLine}</span>
        </span>
      )}
      <DonutRing className="hero-card__ring" value={done / total} size={52} stroke={5} label={`Сбылось ${pct}%`}>
        <span className="num">{pct}%</span>
      </DonutRing>
    </button>
  )
}
