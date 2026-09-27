import { useState } from 'react'
import { ApiError } from '../../api/errors'
import type { Category } from '../../api/types'
import { categoryBackdrop } from '../../lib/categoryStyle'
import { cx } from '../../lib/cx'
import { useData, useMe } from '../../state/data'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { Backdrop } from '../../ui/art'
import { TextField, charCount } from '../../ui/fields'
import { Cell, Section } from '../../ui/layout'
import { Sheet } from '../../ui/Sheet'
import { dismissKeyboard } from '../shared/forms'

// 48 curated emoji, 8 per row.
const EMOJI = [
  '🛍', '✈️', '🎉', '🍽', '🏠', '🎁', '📚', '💫',
  '🚗', '🏖', '🏔', '⛺', '🎮', '🎨', '🎵', '🎬',
  '📷', '💻', '📱', '👗', '👟', '💍', '💄', '🧸',
  '🐶', '🌱', '🌸', '🏋️', '🚴', '⚽', '🧘', '🍷',
  '☕', '🍰', '🍣', '🛋', '🛏', '🔧', '💰', '🎓',
  '❤️', '💞', '⭐', '🔥', '🌍', '🎄', '👶', '🎈',
] as const

interface CategorySheetProps {
  open: boolean
  /** null creates a new category. Keep it stable while the sheet animates out. */
  category: Category | null
  onClose: () => void
  onSaved: (category: Category) => void
}

export function CategorySheet({ open, category, onClose, onSaved }: CategorySheetProps) {
  // A fresh editor per opening, so a cancelled draft never leaks into the next one.
  const [session, setSession] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setSession((s) => s + 1)
  }
  return (
    <Sheet open={open} onClose={onClose} title={category ? 'Категория' : 'Новая категория'}>
      <CategoryEditor key={session} active={open} category={category} onClose={onClose} onSaved={onSaved} />
    </Sheet>
  )
}

function CategoryEditor({
  active,
  category,
  onClose,
  onSaved,
}: {
  active: boolean
  category: Category | null
  onClose: () => void
  onSaved: (category: Category) => void
}) {
  const data = useData()
  const { limits } = useMe()
  const tg = useTelegram()
  const toast = useToast()
  const [name, setName] = useState(category?.name ?? '')
  const [emoji, setEmoji] = useState<string>(category?.emoji ?? EMOJI[0])
  const [error, setError] = useState<{ field: 'name' | 'emoji' | 'form'; message: string } | null>(null)
  const [shake, setShake] = useState(0)
  const [saving, setSaving] = useState(false)

  const fail = (field: 'name' | 'emoji' | 'form', message: string) => {
    setError({ field, message })
    setShake((n) => n + 1)
    tg.haptic.notify('error')
  }

  const save = async () => {
    if (saving) return
    tg.hideKeyboard()
    const trimmed = name.trim()
    if (trimmed === '') return fail('name', 'Название категории обязательно')
    if (charCount(trimmed) > limits.category_name_max) return fail('name', `Не длиннее ${limits.category_name_max} символов`)
    setSaving(true)
    try {
      const input = { name: trimmed, emoji }
      const saved = category ? await data.api.updateCategory(category.id, input) : await data.api.createCategory(input)
      data.putCategory(saved)
      tg.haptic.notify('success')
      toast(category ? 'Сохранено' : 'Категория создана', { tone: 'success' })
      onSaved(saved)
    } catch (err) {
      if (!(err instanceof ApiError) || err.isAuth) return
      const field = err.field === 'name' || err.field === 'emoji' ? err.field : 'form'
      fail(field, err.code === 'conflict' ? 'Такая категория уже есть' : err.message)
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    if (!category) return
    tg.haptic.notify('warning')
    const ok = await tg.confirm(`Удалить «${category.name}»? Мечты из неё останутся — просто без категории.`, 'Удалить')
    if (!ok) return
    try {
      await data.api.deleteCategory(category.id)
      data.dropCategory(category.id)
      void data.refresh('wishes', 'categories')
      toast('Категория удалена', { tone: 'success' })
      onClose()
    } catch (err) {
      if (err instanceof ApiError && !err.isAuth) fail('form', err.message)
    }
  }

  useMainButton(
    active
      ? { text: 'Сохранить', active: name.trim() !== '', progress: saving, onClick: () => void save() }
      : null,
  )

  return (
    <form className="category-editor" noValidate onSubmit={dismissKeyboard(tg)}>
      <div className="category-editor__preview">
        <span className="emoji-tile emoji-tile--lg">
          <Backdrop
            key={emoji}
            emoji={emoji}
            {...categoryBackdrop(category ? { ...category, emoji } : { id: 0, name, emoji, position: 0 })}
            size="tile"
            stickerSize={40}
          />
        </span>
        <span className="category-editor__name">{name.trim() || 'Название'}</span>
      </div>

      <TextField
        label="Название"
        placeholder="Например, Книги"
        value={name}
        onChange={(e) => {
          setName(e.target.value)
          if (error?.field === 'name') setError(null)
        }}
        error={error?.field === 'name' ? error.message : undefined}
        shakeKey={shake}
        max={limits.category_name_max}
        counterFrom={limits.category_name_max - 8}
        autoComplete="off"
        enterKeyHint="done"
      />

      <div className="emoji-grid" role="radiogroup" aria-label="Эмодзи">
        {EMOJI.map((e) => (
          <button
            key={e}
            type="button"
            role="radio"
            aria-checked={emoji === e}
            className={cx('emoji-grid__item', emoji === e && 'emoji-grid__item--selected')}
            onClick={() => {
              if (emoji !== e) tg.haptic.selection()
              setEmoji(e)
            }}
          >
            {e}
          </button>
        ))}
      </div>
      {error && error.field !== 'name' && (
        <p className="form__error" role="alert">
          {error.message}
        </p>
      )}

      {category && (
        <Section>
          <Cell tone="destructive" title="Удалить категорию" onClick={() => void remove()} />
        </Section>
      )}
    </form>
  )
}
