import { useEffect, useState } from 'react'
import { ApiError } from '../../api/errors'
import { RECIPE_BACKDROP, RECIPE_GRADIENT } from '../../lib/categoryStyle'
import { formatByline } from '../../lib/format'
import { recipeEmoji } from '../../lib/recipeEmoji'
import { glueShortWords } from '../../lib/typo'
import { personSlot, useData, useMe } from '../../state/data'
import { useNav } from '../../state/nav'
import { useStillOnTop } from '../../state/screen'
import { useToast } from '../../state/toast'
import { useMainButton, useSecondaryButton, useTelegram } from '../../telegram/hooks'
import { Backdrop } from '../../ui/art'
import { IconDices, IconShuffle } from '../../ui/icons'
import { Cell, InlineAction, Section } from '../../ui/layout'
import { Avatar, Carousel } from '../../ui/media'
import { LinkCell } from '../shared/LinkCell'
import { LoadState } from '../shared/LoadState'
import { useEntity } from '../shared/useEntity'
import { pickRandomRecipe } from './random'
import { recipeLines } from './steps'

export function RecipeDetail({ id, random = false }: { id: number; random?: boolean }) {
  const data = useData()
  const me = useMe()
  const nav = useNav()
  const tg = useTelegram()
  const toast = useToast()
  const lookup = useEntity(data.recipes, id, (recipeId) => data.api.recipe(recipeId))
  const [rolling, setRolling] = useState(false)
  const stillOnTop = useStillOnTop()
  const recipe = lookup.entity

  // The random pick lands with a light tap, together with the kicker's pop.
  useEffect(() => {
    if (random) tg.haptic.impact('light')
  }, [random, id, tg])

  const edit = () => nav.push({ name: 'recipe-form', id })

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

  useMainButton(recipe ? { text: 'Изменить', onClick: edit } : null)
  const hasSecondary = useSecondaryButton(
    recipe && random ? { text: '🎲 Ещё вариант', progress: rolling, onClick: () => void reroll() } : null,
  )

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

  const emoji = recipeEmoji(recipe.title)

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
        <p className="detail__overline">
          <span className="detail__overline-emoji" aria-hidden="true">
            {emoji}
          </span>
          Рецепт
        </p>
        <h1 className="detail__title">{glueShortWords(recipe.title)}</h1>
        <div className="detail__byline">
          <Avatar name={recipe.author.name} slot={personSlot(me, recipe.author.id)} />
          <span>{formatByline(recipe.author.name, recipe.created_at)}</span>
        </div>
      </header>

      {recipe.link && <LinkCell url={recipe.link} title="Открыть рецепт" />}

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

      {!recipe.link && !recipe.body && recipe.images.length === 0 && (
        <p className="detail__hint">Здесь пока только название. Добавьте ссылку, текст или скриншоты.</p>
      )}

      {random && !hasSecondary && (
        <InlineAction icon={<IconShuffle size={18} strokeWidth={2.2} />} onClick={() => void reroll()} disabled={rolling}>
          Ещё вариант
        </InlineAction>
      )}

      <Section className="section--actions">
        <Cell tone="destructive" title="Удалить рецепт" onClick={() => void remove()} />
      </Section>
    </article>
  )
}
