import { useState } from 'react'
import { ApiError } from '../../api/errors'
import type { RecipeTag, TagKind } from '../../api/types'
import { RECIPE_BACKDROP } from '../../lib/categoryStyle'
import { cx } from '../../lib/cx'
import { useData, useMe } from '../../state/data'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { Backdrop } from '../../ui/art'
import { TextField, charCount } from '../../ui/fields'
import { Cell, Section } from '../../ui/layout'
import { Sheet } from '../../ui/Sheet'
import { dismissKeyboard } from '../shared/forms'

// 48 kitchen emoji, 8 per row.
const EMOJI = [
  '🥟', '🥐', '🍝', '🍜', '🍢', '🌮', '🍣', '🍛',
  '🥘', '🍲', '🥗', '🍕', '🍔', '🌯', '🥙', '🧆',
  '🍳', '🥞', '🧇', '🥓', '🍱', '🍙', '🍤', '🦐',
  '🥩', '🍗', '🐟', '🥦', '🥕', '🌶️', '🧀', '🥖',
  '🍰', '🧁', '🍪', '🍫', '🍦', '🍩', '🥧', '🍮',
  '🥤', '☕', '🍵', '🍷', '🍹', '🌙', '☀️', '🔥',
] as const

export const KIND_COPY: Readonly<Record<TagKind, { title: string; create: string; placeholder: string; created: string }>> = {
  cuisine: { title: 'Кухня', create: 'Новая кухня', placeholder: 'Например, Грузинская', created: 'Кухня добавлена' },
  course: { title: 'Тип блюда', create: 'Новый тип блюда', placeholder: 'Например, Гарнир', created: 'Тип блюда добавлен' },
}

interface TagSheetProps {
  open: boolean
  /** null creates a new tag of `kind`. Keep it stable while the sheet animates out. */
  tag: RecipeTag | null
  kind: TagKind
  onClose: () => void
  onSaved: (tag: RecipeTag) => void
}

export function TagSheet({ open, tag, kind, onClose, onSaved }: TagSheetProps) {
  // A fresh editor per opening, so a cancelled draft never leaks into the next one.
  const [session, setSession] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setSession((s) => s + 1)
  }
  const copy = KIND_COPY[tag?.kind ?? kind]
  return (
    <Sheet open={open} onClose={onClose} title={tag ? copy.title : copy.create}>
      <TagEditor key={session} active={open} tag={tag} kind={kind} onClose={onClose} onSaved={onSaved} />
    </Sheet>
  )
}

function TagEditor({
  active,
  tag,
  kind,
  onClose,
  onSaved,
}: {
  active: boolean
  tag: RecipeTag | null
  kind: TagKind
  onClose: () => void
  onSaved: (tag: RecipeTag) => void
}) {
  const data = useData()
  const { limits } = useMe()
  const tg = useTelegram()
  const toast = useToast()
  const copy = KIND_COPY[tag?.kind ?? kind]
  const [name, setName] = useState(tag?.name ?? '')
  const [emoji, setEmoji] = useState<string>(tag?.emoji ?? (kind === 'cuisine' ? '🥘' : '🍽'))
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
    if (trimmed === '') return fail('name', 'Название обязательно')
    if (charCount(trimmed) > limits.category_name_max) return fail('name', `Не длиннее ${limits.category_name_max} символов`)
    setSaving(true)
    try {
      const saved = tag
        ? await data.api.updateRecipeTag(tag.id, { name: trimmed, emoji })
        : await data.api.createRecipeTag({ kind, name: trimmed, emoji })
      data.putTag(saved)
      tg.haptic.notify('success')
      toast(tag ? 'Сохранено' : copy.created, { tone: 'success' })
      onSaved(saved)
    } catch (err) {
      if (!(err instanceof ApiError) || err.isAuth) return
      const field = err.field === 'name' || err.field === 'emoji' ? err.field : 'form'
      fail(field, err.code === 'conflict' ? 'Такой тег уже есть' : err.message)
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    if (!tag) return
    tg.haptic.notify('warning')
    const ok = await tg.confirm(`Удалить «${tag.name}»? Рецепты останутся — просто без этого тега.`, 'Удалить')
    if (!ok) return
    try {
      await data.api.deleteRecipeTag(tag.id)
      data.dropTag(tag.id)
      void data.refresh('recipes', 'tags')
      toast('Тег удалён', { tone: 'success' })
      onClose()
    } catch (err) {
      if (err instanceof ApiError && !err.isAuth) fail('form', err.message)
    }
  }

  useMainButton(active ? { text: 'Сохранить', active: name.trim() !== '', progress: saving, onClick: () => void save() } : null)

  return (
    <form className="category-editor" noValidate onSubmit={dismissKeyboard(tg)}>
      <div className="category-editor__preview">
        <span className="emoji-tile emoji-tile--lg">
          <Backdrop key={emoji} emoji={emoji} {...RECIPE_BACKDROP} size="tile" stickerSize={40} />
        </span>
        <span className="category-editor__name">{name.trim() || 'Название'}</span>
      </div>

      <TextField
        label="Название"
        placeholder={copy.placeholder}
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

      {tag && (
        <Section>
          <Cell tone="destructive" title="Удалить тег" onClick={() => void remove()} />
        </Section>
      )}
    </form>
  )
}
