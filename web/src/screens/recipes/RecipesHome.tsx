import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { ApiError } from '../../api/errors'
import type { Recipe, RecipeTag } from '../../api/types'
import { cx } from '../../lib/cx'
import { RECIPE_GENITIVE_FORMS, plural } from '../../lib/format'
import { readPref, writePref } from '../../lib/prefs'
import {
  RECIPE_SORTS,
  countByTag,
  matchesTags,
  sortRecipes,
  tagMap,
  tagsOfKind,
  type RecipeSort,
  type TagFilter,
} from '../../lib/recipes'
import { matchesQuery } from '../../lib/search'
import { useData } from '../../state/data'
import { useNav } from '../../state/nav'
import { useStillOnTop } from '../../state/screen'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { Sticker } from '../../ui/art'
import { Chip, ChipGroup, ChoicePills, SearchField } from '../../ui/controls'
import { IconShuffle } from '../../ui/icons'
import { EmptyState } from '../../ui/layout'
import { useStuck } from '../../ui/useStuck'
import { RecipeCard } from './RecipeCard'
import { pickRandomRecipe } from './random'

const SORT_PREF = 'recipes-sort'

function initialSort(): RecipeSort {
  const saved = readPref(SORT_PREF)
  return RECIPE_SORTS.find((s) => s.value === saved)?.value ?? 'new'
}

/** Title, text and ingredient names: "курица" finds every recipe that uses it. */
function searchable(r: Recipe): string[] {
  return [r.title, r.body, ...r.ingredients.map((i) => i.name)]
}

/** A tag deleted elsewhere falls back to "all". */
function liveFilter(filter: TagFilter, byId: ReadonlyMap<number, RecipeTag>): TagFilter {
  return typeof filter === 'number' && !byId.has(filter) ? 'all' : filter
}

const REEL_TICK_MS = 90
const REEL_MIN_MS = 600
const REEL_SETTLE_MS = 160
const REEL_TITLES = 7
const PATTERN = ['🍝', '🥗', '🍰', '🍲', '🥞'] as const

const sleep = (ms: number) => new Promise<void>((resolve) => window.setTimeout(resolve, ms))
const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches

export function RecipesHome({ active }: { active: boolean }) {
  const data = useData()
  const nav = useNav()
  const tg = useTelegram()
  const toast = useToast()
  const [query, setQuery] = useState('')
  const [rolling, setRolling] = useState(false)
  const [reel, setReel] = useState<{ tick: number; text: string } | null>(null)
  const timer = useRef(0)
  const stillOnTop = useStillOnTop()
  const { sentinel, stuck } = useStuck()
  const { recipes } = data
  const [rawCuisine, setCuisine] = useState<TagFilter>('all')
  const [rawCourse, setCourse] = useState<TagFilter>('all')
  const [sort, setSort] = useState<RecipeSort>(initialSort)

  useEffect(() => () => window.clearInterval(timer.current), [])

  const byId = useMemo(() => tagMap(data.tags), [data.tags])
  const cuisine = liveFilter(rawCuisine, byId)
  const course = liveFilter(rawCourse, byId)
  const used = useMemo(() => countByTag(recipes), [recipes])
  const searched = useMemo(() => recipes.filter((r) => matchesQuery(query, ...searchable(r))), [recipes, query])
  // Each row counts what its chips would show given the other row's choice.
  const cuisineCounts = useMemo(() => countByTag(searched.filter((r) => matchesTags(r, 'all', course))), [searched, course])
  const courseCounts = useMemo(() => countByTag(searched.filter((r) => matchesTags(r, cuisine, 'all'))), [searched, cuisine])
  const visible = useMemo(
    () => sortRecipes(searched.filter((r) => matchesTags(r, cuisine, course)), sort),
    [searched, cuisine, course, sort],
  )
  // Only tags that some recipe uses become filter chips (plus the selected one).
  const cuisineChips = tagsOfKind(data.tags, 'cuisine').filter((t) => used.has(t.id) || t.id === cuisine)
  const courseChips = tagsOfKind(data.tags, 'course').filter((t) => used.has(t.id) || t.id === course)
  const filtered = cuisine !== 'all' || course !== 'all'

  const changeSort = (next: RecipeSort) => {
    setSort(next)
    writePref(SORT_PREF, next)
  }

  const resetFilters = () => {
    tg.haptic.impact('light')
    setCuisine('all')
    setCourse('all')
    setQuery('')
  }

  useMainButton(
    active ? { text: 'Добавить рецепт', shine: recipes.length === 0, onClick: () => nav.push({ name: 'recipe-form' }) } : null,
  )

  /** A slot-machine reel through local titles while the server picks. */
  const spinReel = (): (() => void) => {
    if (reducedMotion()) {
      setReel({ tick: 0, text: 'Выбираем…' })
      return () => {}
    }
    const titles = [...recipes]
      .sort(() => Math.random() - 0.5)
      .slice(0, REEL_TITLES)
      .map((r) => r.title)
    if (titles.length === 0) return () => {}
    let tick = 0
    const next = () => {
      setReel({ tick, text: titles[tick % titles.length] ?? '' })
      if (tick % 2 === 1) tg.haptic.selection()
      tick += 1
    }
    next()
    timer.current = window.setInterval(next, REEL_TICK_MS)
    return () => window.clearInterval(timer.current)
  }

  const roll = async () => {
    if (rolling) return
    tg.haptic.impact('medium')
    setRolling(true)
    const stop = recipes.length > 0 ? spinReel() : () => {}
    try {
      const [recipe] = await Promise.all([
        pickRandomRecipe(data.api, null, recipes.length),
        sleep(recipes.length > 0 && !reducedMotion() ? REEL_MIN_MS : 0),
      ])
      stop()
      data.putRecipe(recipe)
      if (!reducedMotion()) {
        setReel({ tick: -1, text: recipe.title })
        await sleep(REEL_SETTLE_MS)
      }
      if (stillOnTop()) nav.push({ name: 'recipe', id: recipe.id, random: true })
    } catch (err) {
      stop()
      if (!(err instanceof ApiError) || err.isAuth) return
      tg.haptic.notify('warning')
      toast(err.code === 'not_found' ? 'Сначала добавьте хотя бы один рецепт 🍳' : err.message, { tone: 'error' })
    } finally {
      setRolling(false)
      setReel(null)
    }
  }

  const n = recipes.length
  return (
    <>
      <button
        type="button"
        className={cx('dice-card', rolling && 'dice-card--rolling')}
        onClick={() => void roll()}
        aria-busy={rolling}
        aria-label={n > 0 ? 'Что приготовить? Выбрать случайный рецепт' : 'Что приготовить?'}
      >
        <span className="dice-card__pattern" aria-hidden="true">
          {PATTERN.map((e, k) => (
            <span key={e} style={{ '--k': k } as CSSProperties}>
              {e}
            </span>
          ))}
        </span>
        <span className="dice-card__dice" aria-hidden="true">
          <Sticker emoji="🎲" size={36} tilt={-10} className="dice-card__sticker" />
        </span>
        <span className="dice-card__text" aria-hidden="true">
          <span className="dice-card__title reel">
            <span key={reel?.tick ?? 'idle'} className={cx('reel__item', reel && 'reel__item--spin')}>
              {reel?.text ?? 'Что приготовить?'}
            </span>
          </span>
          <span className="dice-card__hint">
            {n > 1
              ? `Выберем из ${n} ${plural(n, RECIPE_GENITIVE_FORMS)}`
              : n === 1
                ? 'Добавьте ещё один — и начнём выбирать'
                : 'Добавьте рецепты — и мы подскажем'}
          </span>
        </span>
        <span className="dice-card__go" aria-hidden="true">
          <IconShuffle size={18} strokeWidth={2.2} />
        </span>
      </button>

      {n > 0 && (
        <>
          <div className="sticky-sentinel" ref={sentinel} aria-hidden="true" />
          <div className="sticky-bar sticky-bar--search" data-stuck={stuck || undefined}>
            <SearchField value={query} onChange={setQuery} placeholder="Поиск по рецептам и продуктам" label="Поиск по рецептам" />
          </div>
          <div className="recipe-filters">
            {cuisineChips.length > 0 && (
              <ChipGroup label="Кухня">
                <Chip selected={cuisine === 'all'} onSelect={() => setCuisine('all')}>
                  Все кухни
                </Chip>
                {cuisineChips.map((t) => (
                  <Chip key={t.id} selected={cuisine === t.id} emoji={t.emoji} count={cuisineCounts.get(t.id) ?? 0} onSelect={() => setCuisine(t.id)}>
                    {t.name}
                  </Chip>
                ))}
              </ChipGroup>
            )}
            {courseChips.length > 0 && (
              <ChipGroup label="Тип блюда">
                <Chip selected={course === 'all'} onSelect={() => setCourse('all')}>
                  Любое блюдо
                </Chip>
                {courseChips.map((t) => (
                  <Chip key={t.id} selected={course === t.id} emoji={t.emoji} count={courseCounts.get(t.id) ?? 0} onSelect={() => setCourse(t.id)}>
                    {t.name}
                  </Chip>
                ))}
              </ChipGroup>
            )}
            {n > 1 && (
              <div className="recipe-filters__sort">
                <ChoicePills label="Сортировка" value={sort} onChange={changeSort} options={RECIPE_SORTS} />
              </div>
            )}
          </div>
        </>
      )}

      {n === 0 ? (
        <EmptyState emoji="🍳" title="Рецептов пока нет" text="Сохраните ссылку, скриншот или запишите рецепт своими словами." />
      ) : visible.length === 0 ? (
        <EmptyState
          emoji="🔍"
          title="Ничего не нашлось"
          text={query.trim() !== '' ? `По запросу «${query.trim()}» рецептов нет.` : 'С такими фильтрами рецептов нет.'}
        >
          {(filtered || query.trim() !== '') && (
            <button type="button" className="empty__action" onClick={resetFilters}>
              Сбросить фильтры
            </button>
          )}
        </EmptyState>
      ) : (
        <div className="grid grid--recipes" key={`${cuisine}:${course}:${sort}`}>
          {visible.map((r, i) => (
            <RecipeCard
              key={r.id}
              recipe={r}
              index={i}
              byId={byId}
              onOpen={() => {
                tg.haptic.impact('light')
                nav.push({ name: 'recipe', id: r.id })
              }}
            />
          ))}
        </div>
      )}
    </>
  )
}
