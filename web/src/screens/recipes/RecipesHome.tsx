import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { ApiError } from '../../api/errors'
import { cx } from '../../lib/cx'
import { RECIPE_GENITIVE_FORMS, plural } from '../../lib/format'
import { matchesQuery } from '../../lib/search'
import { useData } from '../../state/data'
import { useNav } from '../../state/nav'
import { useStillOnTop } from '../../state/screen'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { Sticker } from '../../ui/art'
import { SearchField } from '../../ui/controls'
import { IconShuffle } from '../../ui/icons'
import { EmptyState } from '../../ui/layout'
import { useStuck } from '../../ui/useStuck'
import { RecipeCard } from './RecipeCard'
import { pickRandomRecipe } from './random'

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

  useEffect(() => () => window.clearInterval(timer.current), [])

  const visible = useMemo(() => recipes.filter((r) => matchesQuery(query, r.title, r.body)), [recipes, query])

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
            <SearchField value={query} onChange={setQuery} placeholder="Поиск по рецептам" label="Поиск по рецептам" />
          </div>
        </>
      )}

      {n === 0 ? (
        <EmptyState emoji="🍳" title="Рецептов пока нет" text="Сохраните ссылку, скриншот или запишите рецепт своими словами." />
      ) : visible.length === 0 ? (
        <EmptyState emoji="🔍" title="Ничего не нашлось" text={`По запросу «${query.trim()}» рецептов нет.`} />
      ) : (
        <div className="grid grid--recipes">
          {visible.map((r, i) => (
            <RecipeCard
              key={r.id}
              recipe={r}
              index={i}
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
