import { useEffect, useEffectEvent, useMemo, useState } from 'react'
import { ApiError } from '../../api/errors'
import type { VkusvillCart, VkusvillMatch } from '../../api/types'
import { cx } from '../../lib/cx'
import { formatMoney } from '../../lib/format'
import { cartLines, estimateCart, initialChoices, isVkusvillBasketUrl, stepCartQuantity, type CartChoice } from '../../lib/vkusvill'
import { useData } from '../../state/data'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { IconCheck } from '../../ui/icons'
import { EmptyState, RetryBanner, Section, Skeleton } from '../../ui/layout'
import { Sheet } from '../../ui/Sheet'

export function VkusvillSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [session, setSession] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setSession((s) => s + 1)
  }
  return (
    <Sheet open={open} onClose={onClose} title="Корзина во ВкусВилле">
      {session > 0 && <CartBuilder key={session} active={open} />}
    </Sheet>
  )
}

type Phase =
  | { kind: 'matching' }
  | { kind: 'failed'; error: ApiError }
  | { kind: 'pick'; matches: VkusvillMatch[] }
  | { kind: 'ready'; cart: VkusvillCart }

/**
 * Match → confirm → basket. The server asks ВкусВилл for up to three products
 * per unchecked item; the user picks and sets quantities; the server builds a
 * shared basket whose link opens on vkusvill.ru.
 */
function CartBuilder({ active }: { active: boolean }) {
  const data = useData()
  const tg = useTelegram()
  const toast = useToast()
  const [phase, setPhase] = useState<Phase>({ kind: 'matching' })
  const [choices, setChoices] = useState<Map<number, CartChoice>>(new Map())
  const [building, setBuilding] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const [lineErrors, setLineErrors] = useState<ReadonlyMap<number, string>>(new Map())
  const { api, shopping } = data

  // Quantities are preset from the list as it is when the answer arrives.
  const onMatched = useEffectEvent((matches: VkusvillMatch[]) => {
    setChoices(initialChoices(matches, shopping))
    setPhase({ kind: 'pick', matches })
  })

  useEffect(() => {
    let cancelled = false
    setPhase({ kind: 'matching' })
    api.vkusvillMatch(null).then(
      (matches) => {
        if (!cancelled) onMatched(matches)
      },
      (err: unknown) => {
        if (cancelled || !(err instanceof ApiError) || err.isAuth) return
        setPhase({ kind: 'failed', error: err })
      },
    )
    return () => {
      cancelled = true
    }
  }, [api, attempt])

  const matches = phase.kind === 'pick' ? phase.matches : []
  const estimate = useMemo(() => estimateCart(matches, choices), [matches, choices])
  const itemNames = useMemo(() => new Map(shopping.map((it) => [it.id, it])), [shopping])

  const choose = (itemId: number, patch: Partial<CartChoice>) => {
    setChoices((prev) => {
      const next = new Map(prev)
      next.set(itemId, { ...(prev.get(itemId) ?? { xmlId: null, quantity: '1' }), ...patch })
      return next
    })
    setLineErrors((e) => {
      if (!e.has(itemId)) return e
      const next = new Map(e)
      next.delete(itemId)
      return next
    })
  }

  const build = async () => {
    if (building || phase.kind !== 'pick') return
    const r = cartLines(phase.matches, choices)
    if (!r.ok) {
      tg.haptic.notify('error')
      setLineErrors(r.errors)
      if (r.message) toast(r.message, { tone: 'error' })
      return
    }
    setBuilding(true)
    try {
      const cart = await api.vkusvillCart(r.lines)
      tg.haptic.notify('success')
      setPhase({ kind: 'ready', cart })
    } catch (err) {
      if (!(err instanceof ApiError) || err.isAuth) return
      tg.haptic.notify('error')
      toast(err.code === 'unavailable' ? 'ВкусВилл сейчас не отвечает — попробуйте чуть позже' : err.message, { tone: 'error' })
    } finally {
      setBuilding(false)
    }
  }

  const openBasket = (url: string) => {
    if (!isVkusvillBasketUrl(url)) {
      toast('Ссылка на корзину выглядит странно — не открываем', { tone: 'error' })
      return
    }
    tg.haptic.impact('light')
    tg.openLink(url)
  }

  const count = estimate?.count ?? 0
  useMainButton(
    !active
      ? null
      : phase.kind === 'matching'
        ? { text: 'Ищем товары…', active: false, progress: true, onClick: () => {} }
        : phase.kind === 'failed'
          ? { text: 'Повторить', onClick: () => setAttempt((n) => n + 1) }
          : phase.kind === 'pick'
            ? {
                text: count > 0 ? `Собрать корзину · ${count}` : 'Выберите товары',
                active: count > 0,
                progress: building,
                onClick: () => void build(),
              }
            : { text: 'Открыть во ВкусВилле', shine: true, onClick: () => openBasket(phase.cart.url) },
  )

  if (phase.kind === 'matching') {
    return (
      <div className="vv" aria-busy="true">
        <p className="vv__lead">Ищем товары из списка во ВкусВилле…</p>
        {[0, 1, 2].map((i) => (
          <div key={i} className="vv__skeleton">
            <Skeleton width="40%" height="12px" radius="6px" delay={i * 80} />
            <Skeleton height="52px" radius="16px" delay={40 + i * 80} />
          </div>
        ))}
      </div>
    )
  }

  if (phase.kind === 'failed') {
    return (
      <div className="vv">
        <RetryBanner
          error={
            phase.error.code === 'unavailable'
              ? new ApiError(503, 'unavailable', 'ВкусВилл сейчас не отвечает')
              : phase.error
          }
          onRetry={() => setAttempt((n) => n + 1)}
        />
        <p className="vv__note">Пока можно искать продукты по одному — кнопкой 🔍 в списке.</p>
      </div>
    )
  }

  if (phase.kind === 'ready') {
    const total = phase.cart.estimated_total
    return (
      <div className="vv vv--ready">
        <EmptyState emoji="🧺" title="Корзина готова" text={total ? `Примерно ${total.formatted} — цены могут немного отличаться.` : undefined}>
          <p className="vv__note">Откроется vkusvill.ru — войдите там, чтобы оформить заказ.</p>
        </EmptyState>
      </div>
    )
  }

  if (phase.matches.length === 0) {
    return (
      <div className="vv">
        <EmptyState emoji="🥬" title="Нечего собирать" text="В списке нет некупленных позиций." />
      </div>
    )
  }

  return (
    <div className="vv">
      <p className="vv__summary num" aria-live="polite">
        {estimate
          ? `≈ ${formatMoney(estimate.minor, estimate.currency)}${estimate.complete ? '' : ' +'} · выбрано ${estimate.count}`
          : 'Ничего не выбрано'}
      </p>
      {phase.matches.map((m) => {
        const choice = choices.get(m.item_id)
        const item = itemNames.get(m.item_id)
        const selected = m.candidates.find((c) => c.xml_id === choice?.xmlId)
        const err = lineErrors.get(m.item_id)
        return (
          <Section
            key={m.item_id}
            header={
              <>
                <span className="vv__query">{m.query}</span>
                {item?.quantity?.formatted && <span className="section__count num">{item.quantity.formatted}</span>}
              </>
            }
            footer={err}
          >
            {m.candidates.length === 0 ? (
              <p className="vv__none">Во ВкусВилле не нашлось — поищите вручную через 🔍</p>
            ) : (
              <div className="vv__options" role="radiogroup" aria-label={`Товар для «${m.query}»`}>
                {m.candidates.map((c) => (
                  <button
                    key={c.xml_id}
                    type="button"
                    role="radio"
                    aria-checked={choice?.xmlId === c.xml_id}
                    className="vv-option"
                    onClick={() => {
                      if (choice?.xmlId !== c.xml_id) tg.haptic.selection()
                      choose(m.item_id, { xmlId: c.xml_id })
                    }}
                  >
                    <span className="vv-option__radio" aria-hidden="true">
                      {choice?.xmlId === c.xml_id && <IconCheck size={13} strokeWidth={3.2} />}
                    </span>
                    <span className="vv-option__main">
                      <span className="vv-option__name">{c.name}</span>
                      {c.weight && <span className="vv-option__weight">{c.weight}</span>}
                    </span>
                    <span className="vv-option__price num">{c.price?.formatted ?? '—'}</span>
                  </button>
                ))}
                <button
                  type="button"
                  role="radio"
                  aria-checked={choice?.xmlId === null}
                  className="vv-option vv-option--skip"
                  onClick={() => {
                    if (choice?.xmlId !== null) tg.haptic.selection()
                    choose(m.item_id, { xmlId: null })
                  }}
                >
                  <span className="vv-option__radio" aria-hidden="true">
                    {choice?.xmlId === null && <IconCheck size={13} strokeWidth={3.2} />}
                  </span>
                  <span className="vv-option__main">
                    <span className="vv-option__name">Не добавлять</span>
                  </span>
                </button>
              </div>
            )}
            {selected && choice && (
              <div className={cx('stepper-row', err && 'stepper-row--error')}>
                <span className="stepper-row__label">Количество</span>
                <div className="qty-stepper">
                  <button
                    type="button"
                    className="qty-stepper__btn"
                    aria-label="Меньше"
                    onClick={() => {
                      tg.haptic.selection()
                      choose(m.item_id, { quantity: stepCartQuantity(choice.quantity, -1, selected.unit) })
                    }}
                  >
                    −
                  </button>
                  <input
                    className="qty-stepper__value num"
                    inputMode="decimal"
                    aria-label={`Количество: ${selected.name}`}
                    value={choice.quantity}
                    onChange={(e) => choose(m.item_id, { quantity: e.target.value })}
                  />
                  <button
                    type="button"
                    className="qty-stepper__btn"
                    aria-label="Больше"
                    onClick={() => {
                      tg.haptic.selection()
                      choose(m.item_id, { quantity: stepCartQuantity(choice.quantity, 1, selected.unit) })
                    }}
                  >
                    +
                  </button>
                </div>
                <span className="stepper-row__unit">{selected.unit}</span>
              </div>
            )}
          </Section>
        )
      })}
      <p className="vv__note">Цены примерные. Корзина откроется на vkusvill.ru — войдите там, чтобы оформить заказ.</p>
    </div>
  )
}
