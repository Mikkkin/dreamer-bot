import { useMemo, useState } from 'react'
import { ApiError } from '../../api/errors'
import type { Recipe } from '../../api/types'
import { cx } from '../../lib/cx'
import { ITEM_FORMS, SERVING_FORMS, countOf, forServings } from '../../lib/format'
import { formatFactor, scaleFactor, scaleIngredient, scaledShoppingItems, stepServings } from '../../lib/servings'
import { shoppingCounts } from '../../lib/shopping'
import { useData, useMe } from '../../state/data'
import { useNav } from '../../state/nav'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { IconCart, IconCheck, IconChevron, IconClipboardList, IconMinus, IconPlus, IconUndo } from '../../ui/icons'
import { Cell, IconTile, Section } from '../../ui/layout'
import { Sheet } from '../../ui/Sheet'

/** The portions the ingredients are shown for; base is the recipe's own (null when unknown). */
export interface Portions {
  base: number | null
  chosen: number | null
}

interface IngredientsSectionProps {
  recipe: Recipe
  portions: Portions
  max: number
  onPortions: (n: number) => void
  /** The servings are unknown: open the form to set them. */
  onAskServings: () => void
  onAdd: () => void
}

/**
 * «Ингредиенты»: the list with amounts for the chosen portions, and the way
 * into the shopping list. Rescaled amounts are tinted and flash once; the
 * recipe itself never changes.
 */
export function IngredientsSection({ recipe, portions, max, onPortions, onAskServings, onAdd }: IngredientsSectionProps) {
  const data = useData()
  const me = useMe()
  const nav = useNav()
  const tg = useTelegram()
  // Bumped on every change of the portions: replays the tint flash.
  const [flash, setFlash] = useState(0)
  const open = shoppingCounts(data.shopping).open
  const { base, chosen } = portions
  const factor = base && chosen ? scaleFactor(base, chosen) : 1
  const scaled = factor !== 1
  const lines = useMemo(() => recipe.ingredients.map((ing) => scaleIngredient(ing, factor, me.unit_forms)), [recipe.ingredients, factor, me.unit_forms])

  const change = (next: number) => {
    if (next === chosen) return
    tg.haptic.selection()
    setFlash((n) => n + 1)
    onPortions(next)
  }

  return (
    <Section
      header={
        <>
          Ингредиенты
          <span className="section__count num">{recipe.ingredients.length}</span>
        </>
      }
    >
      {base && chosen ? (
        <div className="servings-bar">
          <div className="servings" role="group" aria-label="Порции">
            <button
              type="button"
              className="servings__btn"
              aria-label="Меньше порций"
              disabled={chosen <= 1}
              onClick={() => change(stepServings(chosen, -1, max))}
            >
              <IconMinus size={18} strokeWidth={2.6} />
            </button>
            <span className="servings__value" aria-live="polite">
              <span className="servings__count num">{countOf(chosen, SERVING_FORMS)}</span>
              {scaled && <span className="servings__factor num">{formatFactor(factor)}</span>}
            </span>
            <button
              type="button"
              className="servings__btn"
              aria-label="Больше порций"
              disabled={chosen >= max}
              onClick={() => change(stepServings(chosen, 1, max))}
            >
              <IconPlus size={18} strokeWidth={2.6} />
            </button>
          </div>
          {scaled && (
            <button type="button" className="servings__reset" onClick={() => change(base)}>
              <IconUndo size={15} strokeWidth={2.4} />
              Как в рецепте ({base})
            </button>
          )}
        </div>
      ) : (
        <button
          type="button"
          className="servings-hint"
          onClick={() => {
            tg.haptic.impact('light')
            onAskServings()
          }}
        >
          <span>Укажите число порций — появится пересчёт</span>
          <IconChevron size={16} strokeWidth={2.2} />
        </button>
      )}
      <ul className="ingredients">
        {lines.map((line, i) => (
          <li key={i} className="ingredient">
            <span className="ingredient__name">{line.name}</span>
            {line.text && (
              <span
                key={flash}
                className={cx('ingredient__amount num', line.scaled && 'ingredient__amount--scaled', flash > 0 && 'ingredient__amount--flash')}
              >
                {line.text}
              </span>
            )}
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
        subtitle={scaled && chosen ? forServings(chosen) : undefined}
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
  portions: Portions
  onClose: () => void
}

/** Picks which ingredients go to the shopping list; all are selected at first. */
export function AddToShoppingSheet({ open, recipe, portions, onClose }: AddSheetProps) {
  const [session, setSession] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setSession((s) => s + 1)
  }
  return (
    <Sheet open={open} onClose={onClose} title="В список покупок">
      <AddPicker key={session} active={open} recipe={recipe} portions={portions} onClose={onClose} />
    </Sheet>
  )
}

function AddPicker({ active, recipe, portions, onClose }: { active: boolean; recipe: Recipe; portions: Portions; onClose: () => void }) {
  const data = useData()
  const me = useMe()
  const tg = useTelegram()
  const toast = useToast()
  const total = recipe.ingredients.length
  const [picked, setPicked] = useState<ReadonlySet<number>>(() => new Set(recipe.ingredients.map((_, i) => i)))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const allPicked = picked.size === total
  // The portions are fixed while the sheet is open (it covers the stepper).
  const [{ base, chosen }] = useState(portions)
  const factor = base && chosen ? scaleFactor(base, chosen) : 1
  const scaled = factor !== 1 && chosen !== null
  const forWhom = scaled ? forServings(chosen) : ''
  const lines = useMemo(() => recipe.ingredients.map((ing) => scaleIngredient(ing, factor, me.unit_forms)), [recipe.ingredients, factor, me.unit_forms])

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
      // Rescaled amounts go as plain items: the recipe's own positions would add the stored amounts.
      const items = scaled
        ? await data.api.addShopping(scaledShoppingItems(recipe.ingredients, picked, factor))
        : await data.api.addRecipeToShopping(recipe.id, allPicked ? null : [...picked].sort((a, b) => a - b))
      data.putShopping(items)
      tg.haptic.notify('success')
      toast(`Добавлено в покупки: ${countOf(picked.size, ITEM_FORMS)}${scaled ? ` ${forWhom}` : ''}`, { tone: 'success' })
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
          text: picked.size > 0 ? `Добавить ${countOf(picked.size, ITEM_FORMS)}${scaled ? ` · ${forWhom}` : ''}` : 'Выберите продукты',
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
          {scaled && <span className="pick-sheet__for"> · {forWhom}</span>}
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
          {lines.map((line, i) => {
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
                  <span className="pick-row__name">{line.name}</span>
                  {line.text && <span className={cx('pick-row__amount num', line.scaled && 'pick-row__amount--scaled')}>{line.text}</span>}
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
