import { useState } from 'react'
import { ApiError } from '../../api/errors'
import type { Recipe } from '../../api/types'
import { ITEM_FORMS, countOf } from '../../lib/format'
import { shoppingCounts } from '../../lib/shopping'
import { useData } from '../../state/data'
import { useNav } from '../../state/nav'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { IconCart, IconCheck, IconClipboardList } from '../../ui/icons'
import { Cell, IconTile, Section } from '../../ui/layout'
import { Sheet } from '../../ui/Sheet'

/** «Ингредиенты»: the list with amounts, and the way into the shopping list. */
export function IngredientsSection({ recipe, onAdd }: { recipe: Recipe; onAdd: () => void }) {
  const data = useData()
  const nav = useNav()
  const tg = useTelegram()
  const open = shoppingCounts(data.shopping).open
  return (
    <Section
      header={
        <>
          Ингредиенты
          <span className="section__count num">{recipe.ingredients.length}</span>
        </>
      }
    >
      <ul className="ingredients">
        {recipe.ingredients.map((ing, i) => (
          <li key={i} className="ingredient">
            <span className="ingredient__name">{ing.name}</span>
            {ing.formatted && <span className="ingredient__amount num">{ing.formatted}</span>}
          </li>
        ))}
      </ul>
      <Cell
        before={
          <IconTile tone="green">
            <IconCart size={18} strokeWidth={2.1} />
          </IconTile>
        }
        title="В список покупок"
        tone="accent"
        chevron
        onClick={() => {
          tg.haptic.impact('light')
          onAdd()
        }}
      />
      {open > 0 && (
        <Cell
          before={
            <IconTile>
              <IconClipboardList size={18} strokeWidth={2.1} />
            </IconTile>
          }
          title="Открыть список покупок"
          after={<span className="num">{open}</span>}
          chevron
          onClick={() => nav.push({ name: 'shopping' })}
        />
      )}
    </Section>
  )
}

interface AddSheetProps {
  open: boolean
  recipe: Recipe
  onClose: () => void
}

/** Picks which ingredients go to the shopping list; all are selected at first. */
export function AddToShoppingSheet({ open, recipe, onClose }: AddSheetProps) {
  const [session, setSession] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setSession((s) => s + 1)
  }
  return (
    <Sheet open={open} onClose={onClose} title="В список покупок">
      <AddPicker key={session} active={open} recipe={recipe} onClose={onClose} />
    </Sheet>
  )
}

function AddPicker({ active, recipe, onClose }: { active: boolean; recipe: Recipe; onClose: () => void }) {
  const data = useData()
  const tg = useTelegram()
  const toast = useToast()
  const total = recipe.ingredients.length
  const [picked, setPicked] = useState<ReadonlySet<number>>(() => new Set(recipe.ingredients.map((_, i) => i)))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const allPicked = picked.size === total

  const toggle = (i: number, on: boolean) => {
    setPicked((s) => {
      const next = new Set(s)
      if (on) next.add(i)
      else next.delete(i)
      return next
    })
  }

  const submit = async () => {
    if (saving || picked.size === 0) return
    setSaving(true)
    setError(null)
    try {
      const positions = allPicked ? null : [...picked].sort((a, b) => a - b)
      const items = await data.api.addRecipeToShopping(recipe.id, positions)
      data.putShopping(items)
      tg.haptic.notify('success')
      toast(`Добавлено в покупки: ${countOf(picked.size, ITEM_FORMS)}`, { tone: 'success' })
      onClose()
    } catch (err) {
      if (!(err instanceof ApiError) || err.isAuth) return
      tg.haptic.notify('error')
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  useMainButton(
    active
      ? {
          text: picked.size > 0 ? `Добавить ${countOf(picked.size, ITEM_FORMS)}` : 'Выберите продукты',
          active: picked.size > 0,
          progress: saving,
          onClick: () => void submit(),
        }
      : null,
  )

  return (
    <div className="pick-sheet">
      <div className="pick-sheet__bar">
        <span className="pick-sheet__count num">
          {picked.size} из {total}
        </span>
        <button
          type="button"
          className="text-btn"
          onClick={() => {
            tg.haptic.selection()
            setPicked(allPicked ? new Set() : new Set(recipe.ingredients.map((_, i) => i)))
          }}
        >
          {allPicked ? 'Снять все' : 'Выбрать все'}
        </button>
      </div>
      <Section>
        <ul className="pick-list">
          {recipe.ingredients.map((ing, i) => {
            const on = picked.has(i)
            return (
              <li key={i}>
                <button
                  type="button"
                  role="checkbox"
                  aria-checked={on}
                  className="pick-row"
                  onClick={() => {
                    if (on) tg.haptic.selection()
                    else tg.haptic.impact('light')
                    toggle(i, !on)
                  }}
                >
                  <span className="round-check__box" aria-hidden="true">
                    {on && <IconCheck size={14} strokeWidth={3.2} />}
                  </span>
                  <span className="pick-row__name">{ing.name}</span>
                  {ing.formatted && <span className="pick-row__amount num">{ing.formatted}</span>}
                </button>
              </li>
            )
          })}
        </ul>
      </Section>
      {error && (
        <p className="form__error" role="alert">
          {error}
        </p>
      )}
    </div>
  )
}
