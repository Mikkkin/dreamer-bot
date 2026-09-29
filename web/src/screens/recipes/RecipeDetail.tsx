import { useEffect, useMemo, useState } from 'react'
import { ApiError } from '../../api/errors'
import type { Cook, Recipe } from '../../api/types'
import { RECIPE_BACKDROP, RECIPE_GRADIENT } from '../../lib/categoryStyle'
import { TIMES_FORMS, countOf, decimalComma, formatByline, formatDay } from '../../lib/format'
import { recipeEmoji } from '../../lib/recipeEmoji'
import { recipeTags, tagMap } from '../../lib/recipes'
import { glueShortWords } from '../../lib/typo'
import { personSlot, useData, useMe } from '../../state/data'
import { useNav } from '../../state/nav'
import { useStillOnTop } from '../../state/screen'
import { useToast } from '../../state/toast'
import { useMainButton, useSecondaryButton, useTelegram } from '../../telegram/hooks'
import { Backdrop } from '../../ui/art'
import { IconDices, IconPencil, IconShuffle, IconStar } from '../../ui/icons'
import { Cell, IconTile, InlineAction, Section } from '../../ui/layout'
import { Avatar, Carousel } from '../../ui/media'
import { LinkCell } from '../shared/LinkCell'
import { LoadState } from '../shared/LoadState'
import { useEntity } from '../shared/useEntity'
import { CookHistory } from './CookHistory'
import { CookSheet, type CookSheetMode } from './CookSheet'
import { AddToShoppingSheet, IngredientsSection } from './Ingredients'
import { NutritionPanel } from './NutritionPanel'
import { pickRandomRecipe } from './random'
import { recipeLines } from './steps'

type SheetState = { kind: 'none' } | { kind: 'cook' } | { kind: 'shopping' }

/** The cooking history of a recipe, refetched whenever its summary changes. */
function useCooks(recipe: Recipe | null) {
  const data = useData()
  const [state, setState] = useState<{ key: string; cooks: Cook[] | null; error: ApiError | null } | null>(null)
  const [version, setVersion] = useState(0)
  const id = recipe?.id ?? 0
  const count = recipe?.cooking.count ?? 0
  const c = recipe?.cooking
  const signature = c ? `${id}:${c.count}:${c.rating_count}:${c.rating_avg}:${c.last_cooked_at}:${version}` : ''

  useEffect(() => {
    if (id === 0 || count === 0) {
      // The history emptied: a later cooking must not bring the old list back.
      setState(null)
      return
    }
    let cancelled = false
    data.api.cooks(id).then(
      (cooks) => {
        if (!cancelled) setState({ key: signature, cooks, error: null })
      },
      (err: unknown) => {
        if (cancelled || !(err instanceof ApiError) || err.isAuth) return
        setState((s) => ({ key: signature, cooks: s?.cooks ?? null, error: err }))
      },
    )
    return () => {
      cancelled = true
    }
  }, [data.api, id, count, signature])

  // Keep showing the previous history while a refetch is in flight.
  const current = state && state.key.startsWith(`${id}:`) ? state : null
  return {
    cooks: count === 0 ? [] : (current?.cooks ?? null),
    error: current?.key === signature ? current.error : null,
    reload: () => setVersion((v) => v + 1),
  }
}

export function RecipeDetail({ id, random = false }: { id: number; random?: boolean }) {
  const data = useData()
  const me = useMe()
  const nav = useNav()
  const tg = useTelegram()
  const toast = useToast()
  const lookup = useEntity(data.recipes, id, (recipeId) => data.api.recipe(recipeId))
  const [rolling, setRolling] = useState(false)
  const [sheet, setSheet] = useState<SheetState>({ kind: 'none' })
  // The last opened cook sheet, kept while it animates out.
  const [cookMode, setCookMode] = useState<CookSheetMode>({ kind: 'cook' })
  const stillOnTop = useStillOnTop()
  const recipe = lookup.entity
  const history = useCooks(recipe)
  const byId = useMemo(() => tagMap(data.tags), [data.tags])

  // The random pick lands with a light tap, together with the kicker's pop.
  useEffect(() => {
    if (random) tg.haptic.impact('light')
  }, [random, id, tg])

  const edit = () => nav.push({ name: 'recipe-form', id })
  const openCook = (mode: CookSheetMode) => {
    tg.haptic.impact('medium')
    setCookMode(mode)
    setSheet({ kind: 'cook' })
  }
  const closeSheet = () => setSheet({ kind: 'none' })

  const reroll = async () => {
    if (rolling) return
    tg.haptic.impact('medium')
    setRolling(true)
    try {
      const next = await pickRandomRecipe(data.api, id, data.recipes.length)
      data.putRecipe(next)
      if (stillOnTop()) nav.replace({ name: 'recipe', id: next.id, random: true })
    } catch (err) {
      if (!(err instanceof ApiError) || err.isAuth) return
      tg.haptic.notify('error')
      toast(err.message, { tone: 'error' })
    } finally {
      setRolling(false)
    }
  }

  const sheetOpen = sheet.kind !== 'none'
  useMainButton(recipe ? { text: '🍳 Приготовили', shine: recipe.cooking.count === 0, onClick: () => openCook({ kind: 'cook' }) } : null)
  // While a sheet is up it owns the bottom buttons; nothing of the screen may peek through.
  const secondary = !recipe || sheetOpen ? null : random ? { text: '🎲 Ещё вариант', progress: rolling, onClick: () => void reroll() } : { text: 'Изменить', onClick: edit }
  const hasSecondary = useSecondaryButton(secondary)

  if (!recipe) return <LoadState lookup={lookup} missingTitle="Рецепт не найден" missingText="Возможно, его уже удалили." />

  const remove = async () => {
    tg.haptic.notify('warning')
    if (!(await tg.confirm(`Удалить рецепт «${recipe.title}»?`, 'Удалить'))) return
    try {
      await data.api.deleteRecipe(recipe.id)
      if (stillOnTop()) nav.pop()
      data.dropRecipe(recipe.id)
      toast('Удалено', { tone: 'success' })
    } catch (err) {
      tg.haptic.notify('error')
      if (err instanceof ApiError && !err.isAuth) toast(err.message, { tone: 'error' })
    }
  }

  const afterCook = () => {
    closeSheet()
    void data.refreshRecipe(recipe.id)
    history.reload()
  }

  const removeCook = async (cook: Cook) => {
    tg.haptic.notify('warning')
    if (!(await tg.confirm(`Удалить запись о готовке ${formatDay(cook.cooked_at)}? Оценки к ней тоже удалятся.`, 'Удалить'))) return
    try {
      await data.api.deleteCook(recipe.id, cook.id)
      toast('Запись удалена', { tone: 'success' })
    } catch (err) {
      tg.haptic.notify('error')
      if (err instanceof ApiError && !err.isAuth) toast(err.message, { tone: 'error' })
    }
    void data.refreshRecipe(recipe.id)
    history.reload()
  }

  const emoji = recipeEmoji(recipe.title)
  const tags = recipeTags(recipe, byId)
  const { cooking } = recipe

  return (
    <article className="detail">
      {random && (
        <p className="kicker">
          <IconDices size={16} strokeWidth={2.2} />
          Сегодня готовим
        </p>
      )}
      <Carousel
        images={recipe.images}
        alt={recipe.title}
        background={RECIPE_GRADIENT}
        glow={RECIPE_BACKDROP.edge}
        top
        framed
        onOpen={(start) => nav.push({ name: 'viewer', images: recipe.images, start, title: recipe.title })}
        fallback={<Backdrop emoji={emoji} {...RECIPE_BACKDROP} size="hero" seed={recipe.id} />}
      />

      <header className="detail__head">
        {tags.length > 0 ? (
          <ul className="tag-chips" aria-label="Кухня и тип блюда">
            {tags.map((t) => (
              <li key={t.id} className={`tag-chip tag-chip--${t.kind}`}>
                <span className="tag-chip__emoji" aria-hidden="true">
                  {t.emoji}
                </span>
                {t.name}
              </li>
            ))}
          </ul>
        ) : (
          <p className="detail__overline">
            <span className="detail__overline-emoji" aria-hidden="true">
              {emoji}
            </span>
            Рецепт
          </p>
        )}
        <h1 className="detail__title">{glueShortWords(recipe.title)}</h1>
        {cooking.count > 0 || cooking.rating_avg !== null ? (
          <p className="cook-stats">
            {cooking.rating_avg !== null && (
              <span className="cook-stats__rating" aria-label={`Средняя оценка ${decimalComma(cooking.rating_avg)} из 5`}>
                <IconStar size={16} strokeWidth={2} className="icon-fill" />
                <span className="num" aria-hidden="true">
                  {decimalComma(cooking.rating_avg)}
                </span>
              </span>
            )}
            {cooking.count > 0 && <span>готовили {countOf(cooking.count, TIMES_FORMS)}</span>}
            {cooking.last_cooked_at && <span>{formatDay(cooking.last_cooked_at)}</span>}
          </p>
        ) : (
          <p className="cook-stats cook-stats--new">Ещё не готовили — самое время 🍳</p>
        )}
        <div className="detail__byline">
          <Avatar name={recipe.author.name} slot={personSlot(me, recipe.author.id)} />
          <span>{formatByline(recipe.author.name, recipe.created_at)}</span>
        </div>
      </header>

      {recipe.link && <LinkCell url={recipe.link} title="Открыть рецепт" />}

      {recipe.ingredients.length > 0 && <IngredientsSection recipe={recipe} onAdd={() => setSheet({ kind: 'shopping' })} />}

      {recipe.nutrition && <NutritionPanel nutrition={recipe.nutrition} />}

      {recipe.body && (
        <Section header="Как готовить">
          <div className="prose prose--recipe">
            {recipeLines(recipe.body).map((line, i) =>
              line.kind === 'step' ? (
                <p key={i} className="step-line">
                  <span className="step-line__n num" aria-hidden="true">
                    {line.n}
                  </span>
                  <span>
                    <span className="visually-hidden">{line.n}. </span>
                    {line.text}
                  </span>
                </p>
              ) : line.kind === 'gap' ? (
                <span key={i} className="prose__gap" aria-hidden="true" />
              ) : (
                <p key={i}>{line.text}</p>
              ),
            )}
          </div>
        </Section>
      )}

      {cooking.count > 0 && (
        <CookHistory
          cooks={history.cooks}
          error={history.error}
          onRetry={history.reload}
          onRate={(cook) => openCook({ kind: 'rate', cook })}
          onDelete={(cook) => void removeCook(cook)}
        />
      )}

      {!recipe.link && !recipe.body && recipe.images.length === 0 && recipe.ingredients.length === 0 && (
        <p className="detail__hint">Здесь пока только название. Добавьте ссылку, ингредиенты, текст или скриншоты.</p>
      )}

      {random && !hasSecondary && (
        <InlineAction icon={<IconShuffle size={18} strokeWidth={2.2} />} onClick={() => void reroll()} disabled={rolling}>
          Ещё вариант
        </InlineAction>
      )}

      <Section className="section--actions">
        {(random || !hasSecondary) && (
          <Cell
            before={
              <IconTile>
                <IconPencil size={18} />
              </IconTile>
            }
            title="Изменить"
            chevron
            onClick={edit}
          />
        )}
        <Cell tone="destructive" title="Удалить рецепт" onClick={() => void remove()} />
      </Section>

      <CookSheet open={sheet.kind === 'cook'} mode={cookMode} recipe={recipe} onClose={closeSheet} onDone={afterCook} />
      {recipe.ingredients.length > 0 && <AddToShoppingSheet open={sheet.kind === 'shopping'} recipe={recipe} onClose={closeSheet} />}
    </article>
  )
}
