import { useState } from 'react'
import { ApiError } from '../../api/errors'
import type { Saving, Wish } from '../../api/types'
import { parseAmountInput } from '../../lib/amount'
import { savingsCurrency } from '../../lib/savings'
import { useData, useMe } from '../../state/data'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { Segmented } from '../../ui/controls'
import { AmountField, TextField, charCount } from '../../ui/fields'
import { Sheet } from '../../ui/Sheet'
import { dismissKeyboard } from '../shared/forms'

interface SavingSheetProps {
  open: boolean
  wish: Wish
  onClose: () => void
  onSaved: (saving: Saving) => void
}

/** «Отложить»: an amount in the wish's savings currency and an optional note. */
export function SavingSheet({ open, wish, onClose, onSaved }: SavingSheetProps) {
  const [session, setSession] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setSession((s) => s + 1)
  }
  return (
    <Sheet open={open} onClose={onClose} title="Отложить">
      <SavingEditor key={session} active={open} wish={wish} onSaved={onSaved} />
    </Sheet>
  )
}

function SavingEditor({ active, wish, onSaved }: { active: boolean; wish: Wish; onSaved: (saving: Saving) => void }) {
  const data = useData()
  const me = useMe()
  const tg = useTelegram()
  const lock = savingsCurrency(wish, me.default_currency)
  const [amount, setAmount] = useState('')
  const [currency, setCurrency] = useState(lock.currency)
  const [note, setNote] = useState('')
  const [error, setError] = useState<{ field: 'amount' | 'note'; message: string } | null>(null)
  const [shake, setShake] = useState(0)
  const [saving, setSaving] = useState(false)
  const max = me.limits.saving_note_max
  const symbol = me.currencies.find((c) => c.code === currency)?.symbol ?? currency

  const fail = (field: 'amount' | 'note', message: string) => {
    setError({ field, message })
    setShake((n) => n + 1)
    tg.haptic.notify('error')
  }

  const save = async () => {
    if (saving) return
    tg.hideKeyboard()
    const parsed = parseAmountInput(amount)
    if (!parsed.ok) return fail('amount', parsed.message)
    const text = note.trim()
    if (charCount(text) > max) return fail('note', `Не длиннее ${max} символов`)
    setSaving(true)
    try {
      // A locked currency is never taken from the picker.
      const code = lock.locked ? lock.currency : currency
      const saved = await data.api.addSaving(wish.id, { amount: { amount: parsed.decimal, currency: code }, note: text })
      onSaved(saved)
    } catch (err) {
      if (!(err instanceof ApiError) || err.isAuth) return
      fail(err.field === 'note' ? 'note' : 'amount', err.message)
    } finally {
      setSaving(false)
    }
  }

  useMainButton(active ? { text: 'Отложить', active: amount.trim() !== '', progress: saving, onClick: () => void save() } : null)

  return (
    <form className="saving-editor" noValidate onSubmit={dismissKeyboard(tg)}>
      <p className="saving-editor__for">
        на «{wish.title}»
      </p>
      <AmountField
        label="Сумма"
        placeholder="0"
        inputMode="decimal"
        autoComplete="off"
        enterKeyHint="done"
        value={amount}
        onChange={(e) => {
          setAmount(e.target.value)
          if (error?.field === 'amount') setError(null)
        }}
        error={error?.field === 'amount' ? error.message : undefined}
        shakeKey={shake}
        trailing={
          lock.locked ? (
            <span className="currency-lock" title={`Копим в ${currency}`}>
              {symbol}
            </span>
          ) : (
            <Segmented
              className="seg--compact"
              label="Валюта"
              value={currency}
              onChange={setCurrency}
              options={me.currencies.map((c) => ({ value: c.code, label: c.symbol }))}
            />
          )
        }
      />
      {lock.locked && <p className="saving-editor__hint">Копим в {lock.currency} — все взносы в одной валюте.</p>}
      <TextField
        label="Заметка"
        placeholder="Например, с зарплаты"
        value={note}
        onChange={(e) => {
          setNote(e.target.value)
          if (error?.field === 'note') setError(null)
        }}
        error={error?.field === 'note' ? error.message : undefined}
        shakeKey={shake}
        max={max}
        counterFrom={max - 20}
        autoComplete="off"
        enterKeyHint="done"
      />
    </form>
  )
}
