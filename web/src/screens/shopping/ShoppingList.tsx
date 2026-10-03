import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { ApiError } from '../../api/errors'
import type { ShoppingItem } from '../../api/types'
import { cx } from '../../lib/cx'
import { ITEM_FORMS, countOf } from '../../lib/format'
import { readPref, writePref } from '../../lib/prefs'
import { formatQuantity, parseItemText } from '../../lib/quantity'
import { LatestRequest, shoppingCounts } from '../../lib/shopping'
import { useData, useMe } from '../../state/data'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { RoundCheck } from '../../ui/controls'
import { charCount } from '../../ui/fields'
import { IconChevronDown, IconPlus } from '../../ui/icons'
import { EmptyState, Section } from '../../ui/layout'
import { ItemSheet } from './ItemSheet'

const BOUGHT_PREF = 'shopping-bought-collapsed'

/** «Покупки»: the couple's shared checklist. */
export function ShoppingList() {
  const data = useData()
  const me = useMe()
  const tg = useTelegram()
  const toast = useToast()
  const { limits } = me
  const [text, setText] = useState('')
  const [adding, setAdding] = useState(false)
  const [clearing, setClearing] = useState(false)
  const [collapsed, setCollapsed] = useState(() => readPref(BOUGHT_PREF) === '1')
  const [editing, setEditing] = useState<{ open: boolean; item: ShoppingItem | null }>({ open: false, item: null })
  const input = useRef<HTMLInputElement>(null)
  const toggles = useRef(new LatestRequest())
  const { refresh } = data

  // Pick up what the partner changed.
  useEffect(() => {
    void refresh('shopping')
  }, [refresh])

  const recipeTitles = useMemo(() => new Map(data.recipes.map((r) => [r.id, r.title])), [data.recipes])
  const open = data.shopping.filter((it) => !it.checked)
  const bought = data.shopping.filter((it) => it.checked)
  const counts = shoppingCounts(data.shopping)
  const parsed = text.trim() === '' ? null : parseItemText(text, me.units)

  const clear = async () => {
    if (clearing || counts.checked === 0) return
    setClearing(true)
    const ids = bought.map((it) => it.id)
    try {
      const removed = await data.api.clearCheckedShopping()
      data.dropShopping(ids)
      tg.haptic.notify('success')
      toast(`Убрано: ${countOf(removed, ITEM_FORMS)}`, { tone: 'success' })
      void refresh('shopping')
    } catch (err) {
      tg.haptic.notify('error')
      if (err instanceof ApiError && !err.isAuth) toast(err.message, { tone: 'error' })
    } finally {
      setClearing(false)
    }
  }

  useMainButton(
    counts.checked > 0 && !editing.open
      ? { text: `Очистить купленное (${counts.checked})`, progress: clearing, onClick: () => void clear() }
      : null,
  )

  const add = async (e: FormEvent) => {
    e.preventDefault()
    if (adding || !parsed) return
    if (parsed.name === '') return
    if (charCount(parsed.name) > limits.item_name_max) {
      tg.haptic.notify('error')
      toast(`Название не длиннее ${limits.item_name_max} символов`, { tone: 'error' })
      return
    }
    if (data.shopping.length >= limits.shopping_items_max) {
      tg.haptic.notify('error')
      toast(`В списке не больше ${limits.shopping_items_max} позиций — очистите купленное`, { tone: 'error' })
      return
    }
    setAdding(true)
    try {
      const items = await data.api.addShopping([{ name: parsed.name, amount: parsed.amount, unit: parsed.unit }])
      data.putShopping(items)
      tg.haptic.impact('light')
      setText('')
      input.current?.focus()
    } catch (err) {
      tg.haptic.notify('error')
      if (err instanceof ApiError && !err.isAuth) toast(err.message, { tone: 'error' })
    } finally {
      setAdding(false)
    }
  }

  const toggle = async (item: ShoppingItem, checked: boolean) => {
    const n = toggles.current.begin(item.id)
    data.putShopping([{ ...item, checked }])
    try {
      const saved = await data.api.updateShopping(item.id, { checked })
      if (toggles.current.isLatest(item.id, n)) data.putShopping([saved])
    } catch (err) {
      if (!toggles.current.isLatest(item.id, n)) return
      if (err instanceof ApiError && err.code === 'not_found') {
        data.dropShopping([item.id])
        return
      }
      data.putShopping([item])
      tg.haptic.notify('error')
      if (err instanceof ApiError && !err.isAuth) toast(err.message, { tone: 'error' })
    }
  }

  const toggleBought = () => {
    tg.haptic.selection()
    setCollapsed((c) => {
      writePref(BOUGHT_PREF, c ? '0' : '1')
      return !c
    })
  }

  const row = (item: ShoppingItem) => {
    const qty = item.quantity ? formatQuantity(item.quantity.amount, item.quantity.unit, me.unit_forms) : ''
    const source = item.recipe_id !== null ? recipeTitles.get(item.recipe_id) : undefined
    return (
      <li key={item.id} className={cx('shop-row', item.checked && 'shop-row--checked')}>
        <RoundCheck checked={item.checked} onChange={(on) => void toggle(item, on)} label={item.name} />
        <button
          type="button"
          className="shop-row__main"
          onClick={() => {
            tg.haptic.impact('light')
            setEditing({ open: true, item })
          }}
        >
          <span className="shop-row__name">{item.name}</span>
          {source && <span className="shop-row__source">из «{source}»</span>}
        </button>
        {qty && <span className="shop-row__qty num">{qty}</span>}
      </li>
    )
  }

  return (
    <div className="page shopping">
      <h1 className="page__title">Покупки</h1>
      <p className="page__subtitle">
        {counts.open > 0 ? `Купить: ${countOf(counts.open, ITEM_FORMS)}` : data.shopping.length > 0 ? 'Всё куплено 🎉' : 'Список общий — видно вам обоим'}
      </p>

      <form className="add-item" onSubmit={(e) => void add(e)}>
        <label className="add-item__box">
          <span className="visually-hidden">Добавить продукт</span>
          <input
            ref={input}
            className="add-item__input"
            placeholder="Добавить: молоко 1 л"
            value={text}
            maxLength={limits.item_name_max * 2}
            autoComplete="off"
            enterKeyHint="send"
            onChange={(e) => setText(e.target.value)}
          />
          <button type="submit" className="add-item__button" aria-label="Добавить" disabled={!parsed || parsed.name === '' || adding}>
            {adding ? <span className="spinner spinner--on-accent" aria-hidden="true" /> : <IconPlus size={20} strokeWidth={2.6} />}
          </button>
        </label>
        {parsed && (parsed.amount !== null || parsed.unit !== null) && (
          <p className="add-item__hint">
            Добавится: <b>{parsed.name}</b> · {formatQuantity(parsed.amount, parsed.unit, me.unit_forms)}
          </p>
        )}
      </form>

      {data.shopping.length === 0 ? (
        <EmptyState emoji="🛒" title="Список покупок пуст" text="Добавьте продукты сверху или из ингредиентов рецепта — список общий для вас двоих." />
      ) : (
        <>
          {open.length > 0 && (
            <Section>
              <ul className="shop-list">{open.map(row)}</ul>
            </Section>
          )}
          {bought.length > 0 && (
            <section className="section">
              <h2 className="section__header">
                <button type="button" className="bought-toggle" aria-expanded={!collapsed} onClick={toggleBought}>
                  Куплено
                  <span className="num">{bought.length}</span>
                  <span className={cx('bought-toggle__chevron', collapsed && 'bought-toggle__chevron--closed')} aria-hidden="true">
                    <IconChevronDown size={16} strokeWidth={2.4} />
                  </span>
                </button>
              </h2>
              {!collapsed && (
                <div className="section__body">
                  <ul className="shop-list">{bought.map(row)}</ul>
                </div>
              )}
            </section>
          )}
        </>
      )}

      <ItemSheet open={editing.open} item={editing.item} onClose={() => setEditing((s) => ({ ...s, open: false }))} />
    </div>
  )
}
