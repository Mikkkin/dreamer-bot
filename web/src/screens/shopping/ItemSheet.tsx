import { useState } from 'react'
import { ApiError } from '../../api/errors'
import type { ShoppingItem } from '../../api/types'
import { amountForEdit, checkQuantity, TO_TASTE } from '../../lib/quantity'
import { useData, useMe } from '../../state/data'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { TextField, charCount } from '../../ui/fields'
import { IconChevronDown } from '../../ui/icons'
import { Cell, Section } from '../../ui/layout'
import { Sheet } from '../../ui/Sheet'
import { dismissKeyboard } from '../shared/forms'

interface ItemSheetProps {
  open: boolean
  /** Keep it stable while the sheet animates out. */
  item: ShoppingItem | null
  onClose: () => void
}

/** Edits one line of the shopping list: name, amount, unit — or removes it. */
export function ItemSheet({ open, item, onClose }: ItemSheetProps) {
  const [session, setSession] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setSession((s) => s + 1)
  }
  return (
    <Sheet open={open} onClose={onClose} title="Покупка">
      {item && <ItemEditor key={session} active={open} item={item} onClose={onClose} />}
    </Sheet>
  )
}

function ItemEditor({ active, item, onClose }: { active: boolean; item: ShoppingItem; onClose: () => void }) {
  const data = useData()
  const me = useMe()
  const tg = useTelegram()
  const toast = useToast()
  const max = me.limits.item_name_max
  const [name, setName] = useState(item.name)
  const [amount, setAmount] = useState(amountForEdit(item.quantity?.amount ?? null))
  const [unit, setUnit] = useState(item.quantity?.unit ?? '')
  const [error, setError] = useState<{ field: 'name' | 'amount' | 'form'; message: string } | null>(null)
  const [shake, setShake] = useState(0)
  const [saving, setSaving] = useState(false)
  const toTaste = unit === TO_TASTE

  const fail = (field: 'name' | 'amount' | 'form', message: string) => {
    setError({ field, message })
    setShake((n) => n + 1)
    tg.haptic.notify('error')
  }

  const save = async () => {
    if (saving) return
    tg.hideKeyboard()
    const trimmed = name.replace(/\s+/gu, ' ').trim()
    if (trimmed === '') return fail('name', 'Укажите название')
    if (charCount(trimmed) > max) return fail('name', `Не длиннее ${max} символов`)
    const q = checkQuantity(amount, unit, me.units)
    if (!q.ok) return fail('amount', q.message)
    setSaving(true)
    try {
      const saved = await data.api.updateShopping(item.id, { name: trimmed, amount: q.value.amount, unit: q.value.unit })
      data.putShopping([saved])
      tg.haptic.notify('success')
      onClose()
    } catch (err) {
      if (!(err instanceof ApiError) || err.isAuth) return
      if (err.code === 'not_found') {
        data.dropShopping([item.id])
        toast('Этой позиции уже нет в списке', { tone: 'error' })
        onClose()
        return
      }
      fail(err.field === 'name' ? 'name' : err.field === 'amount' || err.field === 'unit' ? 'amount' : 'form', err.message)
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    tg.haptic.impact('medium')
    try {
      await data.api.deleteShopping(item.id)
      data.dropShopping([item.id])
      toast('Убрано из списка', { tone: 'success' })
      onClose()
    } catch (err) {
      if (err instanceof ApiError && err.code === 'not_found') {
        data.dropShopping([item.id])
        onClose()
        return
      }
      if (err instanceof ApiError && !err.isAuth) fail('form', err.message)
    }
  }

  useMainButton(active ? { text: 'Сохранить', active: name.trim() !== '', progress: saving, onClick: () => void save() } : null)

  return (
    <form className="item-editor" noValidate onSubmit={dismissKeyboard(tg)}>
      <TextField
        label="Название"
        value={name}
        onChange={(e) => {
          setName(e.target.value)
          if (error?.field === 'name') setError(null)
        }}
        error={error?.field === 'name' ? error.message : undefined}
        shakeKey={shake}
        max={max}
        counterFrom={max - 10}
        autoComplete="off"
        enterKeyHint="done"
      />
      <div className="item-editor__qty">
        <TextField
          label="Количество"
          inputMode="decimal"
          placeholder={toTaste ? '—' : 'Например, 1,5'}
          value={toTaste ? '' : amount}
          disabled={toTaste}
          onChange={(e) => {
            setAmount(e.target.value)
            if (error?.field === 'amount') setError(null)
          }}
          error={error?.field === 'amount' ? error.message : undefined}
          shakeKey={shake}
          autoComplete="off"
          enterKeyHint="done"
        />
        <span className="unit-select unit-select--field">
          <select
            aria-label="Единица"
            value={unit}
            onChange={(e) => {
              tg.haptic.selection()
              setUnit(e.target.value)
              if (e.target.value === TO_TASTE) setAmount('')
              if (error?.field === 'amount') setError(null)
            }}
          >
            <option value="">ед.</option>
            {me.units.map((u) => (
              <option key={u} value={u}>
                {u}
              </option>
            ))}
          </select>
          <IconChevronDown size={14} strokeWidth={2.4} />
        </span>
      </div>
      {error?.field === 'form' && (
        <p className="form__error" role="alert">
          {error.message}
        </p>
      )}
      <Section>
        <Cell tone="destructive" title="Убрать из списка" onClick={() => void remove()} />
      </Section>
    </form>
  )
}
