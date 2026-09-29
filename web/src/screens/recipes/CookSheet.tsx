import { useState } from 'react'
import { ApiError } from '../../api/errors'
import type { Cook, Recipe } from '../../api/types'
import { STAR_COLORS, celebrate } from '../../lib/confetti'
import { ratingOf } from '../../lib/recipes'
import { useData, useMe } from '../../state/data'
import { useToast } from '../../state/toast'
import { useMainButton, useSecondaryButton, useTelegram } from '../../telegram/hooks'
import { StarRating } from '../../ui/controls'
import { TextArea, charCount } from '../../ui/fields'
import { InlineAction } from '../../ui/layout'
import { Sheet } from '../../ui/Sheet'
import { dismissKeyboard } from '../shared/forms'

/** «Приготовили» records a new cooking; «Оценить» rates an existing one. */
export type CookSheetMode = { kind: 'cook' } | { kind: 'rate'; cook: Cook }

const MOOD = ['', 'Не понравилось', 'Так себе', 'Нормально', 'Вкусно', 'Восторг!'] as const

interface CookSheetProps {
  open: boolean
  mode: CookSheetMode
  recipe: Recipe
  onClose: () => void
  onDone: (cook: Cook) => void
}

export function CookSheet({ open, mode, recipe, onClose, onDone }: CookSheetProps) {
  // A fresh editor per opening, so the stars of the last cooking never linger.
  const [session, setSession] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setSession((s) => s + 1)
  }
  return (
    <Sheet open={open} onClose={onClose} title={mode.kind === 'cook' ? 'Как получилось?' : 'Ваша оценка'}>
      <CookEditor key={session} active={open} mode={mode} recipe={recipe} onDone={onDone} />
    </Sheet>
  )
}

function CookEditor({ active, mode, recipe, onDone }: { active: boolean; mode: CookSheetMode; recipe: Recipe; onDone: (cook: Cook) => void }) {
  const data = useData()
  const me = useMe()
  const tg = useTelegram()
  const toast = useToast()
  const mine = mode.kind === 'rate' ? ratingOf(mode.cook, me.user.id) : undefined
  const [stars, setStars] = useState(mine?.stars ?? 0)
  const [comment, setComment] = useState(mine?.comment ?? '')
  const [error, setError] = useState<string | null>(null)
  const [shake, setShake] = useState(0)
  const [saving, setSaving] = useState<'rated' | 'plain' | null>(null)
  const max = me.limits.rating_comment_max

  const submit = async (rated: boolean) => {
    if (saving) return
    tg.hideKeyboard()
    const text = comment.trim()
    if (rated && (stars < 1 || stars > 5)) {
      setError('Выберите от 1 до 5 звёзд')
      tg.haptic.notify('error')
      return
    }
    if (rated && charCount(text) > max) {
      setError(`Комментарий не длиннее ${max} символов`)
      setShake((n) => n + 1)
      tg.haptic.notify('error')
      return
    }
    setSaving(rated ? 'rated' : 'plain')
    try {
      const rating = { stars, comment: text }
      const cook =
        mode.kind === 'cook'
          ? await data.api.cookRecipe(recipe.id, rated ? rating : null)
          : await data.api.rateCook(recipe.id, mode.cook.id, rating)
      tg.haptic.notify('success')
      if (rated) celebrate({ light: true, colors: STAR_COLORS })
      toast(mode.kind === 'cook' ? 'Приготовлено 🍳' : 'Оценка сохранена', { tone: 'success' })
      onDone(cook)
    } catch (err) {
      if (!(err instanceof ApiError) || err.isAuth) return
      setError(err.message)
      setShake((n) => n + 1)
      tg.haptic.notify('error')
    } finally {
      setSaving(null)
    }
  }

  useMainButton(
    active
      ? { text: 'Сохранить', active: stars > 0, progress: saving === 'rated', onClick: () => void submit(true) }
      : null,
  )
  const plain = active && mode.kind === 'cook'
  const hasSecondary = useSecondaryButton(
    plain ? { text: 'Без оценки', active: saving === null, progress: saving === 'plain', onClick: () => void submit(false) } : null,
  )

  return (
    <form className="cook-editor" noValidate onSubmit={dismissKeyboard(tg)}>
      <p className="cook-editor__recipe">{recipe.title}</p>
      <StarRating
        value={stars}
        onChange={(n) => {
          setStars(n)
          setError(null)
        }}
      />
      <p className="cook-editor__mood" aria-live="polite">
        {MOOD[stars] ?? ''}
      </p>
      <TextArea
        label="Комментарий"
        placeholder="Что понравилось, что поменять в следующий раз…"
        value={comment}
        onChange={(e) => setComment(e.target.value)}
        max={max}
        counterFrom={max - 40}
        minRows={2}
        shakeKey={shake}
        error={error ?? undefined}
        hint="Необязательно"
      />
      {plain && !hasSecondary && (
        <InlineAction onClick={() => void submit(false)} disabled={saving !== null}>
          Без оценки
        </InlineAction>
      )}
    </form>
  )
}
