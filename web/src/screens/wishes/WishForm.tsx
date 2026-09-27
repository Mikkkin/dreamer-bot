import { useMemo, useState } from 'react'
import { ApiError } from '../../api/errors'
import type { Me, Status, Wish, WishInput } from '../../api/types'
import { amountForInput, parseAmountInput } from '../../lib/amount'
import { celebrate } from '../../lib/confetti'
import { linkHost, normalizeLinkInput } from '../../lib/links'
import { usePhotoDraft } from '../../media/usePhotoDraft'
import { useData, useMe } from '../../state/data'
import { useNav } from '../../state/nav'
import { useStillOnTop } from '../../state/screen'
import { useToast } from '../../state/toast'
import { useMainButton, useTelegram } from '../../telegram/hooks'
import { Chip, ChipGroup, Segmented, StatusStepper, SwitchRow } from '../../ui/controls'
import { AmountField, TextArea, TextField, charCount } from '../../ui/fields'
import { IconCoins, IconFlame, IconLink, IconPlus } from '../../ui/icons'
import { IconTile, Section } from '../../ui/layout'
import { PhotoEditor } from '../../ui/PhotoEditor'
import { CategorySheet } from '../categories/CategorySheet'
import { dismissKeyboard, fieldOf, uploadFailureText, useLeaveGuard, type FieldErrors } from '../shared/forms'
import { LoadState } from '../shared/LoadState'
import { useEntity } from '../shared/useEntity'

interface Fields {
  title: string
  categoryId: number | null
  priceOn: boolean
  amount: string
  currency: string
  linkOn: boolean
  link: string
  hot: boolean
  note: string
  status: Status
}

const FIELD_KEYS = ['title', 'category_id', 'price', 'currency', 'link', 'note', 'status'] as const
type FieldKey = (typeof FIELD_KEYS)[number]

/** Which error an edit of a field clears. */
const ERROR_OF: Readonly<Partial<Record<keyof Fields, FieldKey>>> = {
  title: 'title',
  categoryId: 'category_id',
  priceOn: 'price',
  amount: 'price',
  currency: 'currency',
  linkOn: 'link',
  link: 'link',
  note: 'note',
  status: 'status',
}

function initialFields(wish: Wish | null, me: Me, categoryId: number | null): Fields {
  return {
    title: wish?.title ?? '',
    categoryId: wish ? wish.category_id : categoryId,
    priceOn: wish?.price != null,
    amount: wish?.price ? amountForInput(wish.price.amount) : '',
    currency: wish?.price?.currency ?? me.default_currency,
    linkOn: wish?.link != null,
    link: wish?.link ?? '',
    hot: wish?.hot ?? false,
    note: wish?.note ?? '',
    status: wish?.status ?? 'want',
  }
}

export function WishForm({ id, categoryId }: { id?: number; categoryId?: number | null }) {
  const data = useData()
  const lookup = useEntity(data.wishes, id ?? null, (wishId) => data.api.wish(wishId))
  if (id === undefined) return <WishFormBody wish={null} categoryId={categoryId ?? null} />
  if (!lookup.entity) return <LoadState lookup={lookup} missingTitle="Мечта не найдена" missingText="Возможно, её уже удалили." />
  return <WishFormBody wish={lookup.entity} categoryId={null} />
}

function WishFormBody({ wish, categoryId }: { wish: Wish | null; categoryId: number | null }) {
  const data = useData()
  const me = useMe()
  const nav = useNav()
  const tg = useTelegram()
  const toast = useToast()
  const { limits } = me
  const isEdit = wish !== null

  const [initial] = useState(() => initialFields(wish, me, categoryId))
  const [fields, setFields] = useState(initial)
  const [errors, setErrors] = useState<FieldErrors<FieldKey>>({})
  const [shake, setShake] = useState(0)
  const [saving, setSaving] = useState(false)
  const [savedId, setSavedId] = useState<number | null>(wish?.id ?? null)
  const [sheetOpen, setSheetOpen] = useState(false)
  const photos = usePhotoDraft(wish?.images ?? [], limits.images_per_wish)
  const stillOnTop = useStillOnTop()

  const set = <K extends keyof Fields>(key: K, value: Fields[K]) => {
    setFields((f) => ({ ...f, [key]: value }))
    const errorKey = ERROR_OF[key]
    setErrors((e) => (errorKey && e[errorKey] ? { ...e, [errorKey]: undefined } : e))
  }

  const dirty = useMemo(() => JSON.stringify(fields) !== JSON.stringify(initial), [fields, initial]) || photos.dirty
  useLeaveGuard(dirty, saving)

  const fail = (next: FieldErrors<FieldKey>) => {
    setErrors(next)
    setShake((n) => n + 1)
    tg.haptic.notify('error')
  }

  const validate = (): { input: WishInput } | { errors: FieldErrors<FieldKey> } => {
    const errs: FieldErrors<FieldKey> = {}
    const title = fields.title.trim()
    if (title === '') errs.title = 'Название обязательно'
    else if (charCount(title) > limits.title_max) errs.title = `Не длиннее ${limits.title_max} символов`

    let price: WishInput['price'] = null
    if (fields.priceOn) {
      const r = parseAmountInput(fields.amount)
      if (r.ok) price = { amount: r.decimal, currency: fields.currency }
      else errs.price = r.message
    }
    let link: string | null = null
    if (fields.linkOn) {
      const r = normalizeLinkInput(fields.link, limits.link_max)
      if (r.ok) link = r.url
      else errs.link = r.message
    }
    if (charCount(fields.note.trim()) > limits.note_max) errs.note = `Не длиннее ${limits.note_max} символов`

    if (Object.keys(errs).length > 0) return { errors: errs }
    return { input: { title, note: fields.note.trim(), category_id: fields.categoryId, link, price, hot: fields.hot } }
  }

  const save = async () => {
    if (saving) return
    tg.hideKeyboard()
    const result = validate()
    if ('errors' in result) return fail(result.errors)
    setErrors({})
    setSaving(true)
    try {
      let saved = savedId === null ? await data.api.createWish(result.input) : await data.api.updateWish(savedId, result.input)
      setSavedId(saved.id)
      const becameDone = isEdit && fields.status !== saved.status && fields.status === 'done'
      if (isEdit && fields.status !== saved.status) saved = await data.api.setWishStatus(saved.id, fields.status)
      data.putWish(saved)

      const failures = await photos.sync(data.api, 'wishes', saved.id)
      await data.refresh('wishes')
      if (failures.length > 0) {
        tg.haptic.notify('error')
        await tg.alert(uploadFailureText(failures, limits.image_max_bytes))
        return
      }
      tg.haptic.notify('success')
      if (becameDone) celebrate()
      if (stillOnTop()) nav.pop()
      toast('Сохранено', { tone: 'success' })
    } catch (err) {
      if (!(err instanceof ApiError) || err.isAuth) return
      fail({ [fieldOf(err.field, FIELD_KEYS)]: err.message })
    } finally {
      setSaving(false)
    }
  }

  useMainButton({
    text: 'Сохранить',
    active: fields.title.trim() !== '',
    progress: saving,
    onClick: () => void save(),
  })

  const linkCheck = fields.linkOn && fields.link.trim() !== '' ? normalizeLinkInput(fields.link, limits.link_max) : null
  const host = linkCheck?.ok ? linkHost(linkCheck.url) : ''

  return (
    <form className="form" noValidate onSubmit={dismissKeyboard(tg)}>
      <h1 className="form__title">{isEdit ? 'Изменить мечту' : 'Новая мечта'}</h1>

      <TextField
        label="Название"
        placeholder="Например, поездка в Рим"
        value={fields.title}
        onChange={(e) => set('title', e.target.value)}
        error={errors.title}
        shakeKey={shake}
        max={limits.title_max}
        counterFrom={limits.title_max - 20}
        autoComplete="off"
        enterKeyHint="done"
      />

      <Section header="Категория" footer={errors.category_id}>
        <div className="section__pad">
          <ChipGroup label="Категория" wrap>
            <Chip selected={fields.categoryId === null} onSelect={() => set('categoryId', null)}>
              Без категории
            </Chip>
            {data.categories.map((c) => (
              <Chip key={c.id} selected={fields.categoryId === c.id} emoji={c.emoji} onSelect={() => set('categoryId', c.id)}>
                {c.name}
              </Chip>
            ))}
            <Chip action onSelect={() => setSheetOpen(true)}>
              <IconPlus size={16} strokeWidth={2.4} />
              Своя
            </Chip>
          </ChipGroup>
        </div>
      </Section>

      <PhotoEditor label="Фото" draft={photos} max={limits.images_per_wish} alt={fields.title || 'Мечта'} disabled={saving} />

      <Section header="Детали">
        <SwitchRow
          label="Указать сумму"
          before={
            <IconTile tone="green">
              <IconCoins size={18} strokeWidth={2.1} />
            </IconTile>
          }
          checked={fields.priceOn}
          onChange={(on) => set('priceOn', on)}
        >
          <AmountField
            label="Сумма"
            placeholder="0"
            inputMode="decimal"
            value={fields.amount}
            onChange={(e) => set('amount', e.target.value)}
            error={errors.price ?? errors.currency}
            shakeKey={shake}
            autoComplete="off"
            enterKeyHint="done"
            trailing={
              <Segmented
                className="seg--compact"
                label="Валюта"
                value={fields.currency}
                onChange={(c) => set('currency', c)}
                options={me.currencies.map((c) => ({ value: c.code, label: c.symbol }))}
              />
            }
          />
        </SwitchRow>
        <SwitchRow
          label="Добавить ссылку"
          before={
            <IconTile tone="accent">
              <IconLink size={18} strokeWidth={2.1} />
            </IconTile>
          }
          checked={fields.linkOn}
          onChange={(on) => set('linkOn', on)}
        >
          <TextField
            label="Ссылка"
            type="url"
            inputMode="url"
            placeholder="https://"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            autoComplete="off"
            value={fields.link}
            onChange={(e) => set('link', e.target.value)}
            error={errors.link}
            shakeKey={shake}
            hint={host ? `Откроется: ${host}` : undefined}
            enterKeyHint="done"
          />
        </SwitchRow>
        <SwitchRow
          label="Очень хочется"
          before={
            <IconTile tone="orange">
              <IconFlame size={18} strokeWidth={2.1} />
            </IconTile>
          }
          checked={fields.hot}
          onChange={(on) => set('hot', on)}
          haptic="rigid"
        />
      </Section>

      <div className="group">
        <h2 className="section__header">Заметка</h2>
        <TextArea
          label="Заметка"
          hideLabel
          placeholder="Детали, идеи, размеры…"
          value={fields.note}
          onChange={(e) => set('note', e.target.value)}
          error={errors.note}
          shakeKey={shake}
          max={limits.note_max}
          counterFrom={limits.note_max - 200}
          minRows={3}
        />
      </div>

      {isEdit && (
        <Section header="Путь мечты" footer={errors.status}>
          <StatusStepper value={fields.status} onChange={(s) => set('status', s)} />
        </Section>
      )}

      {errors.form && (
        <p className="form__error" role="alert">
          {errors.form}
        </p>
      )}

      <CategorySheet
        open={sheetOpen}
        category={null}
        onClose={() => setSheetOpen(false)}
        onSaved={(c) => {
          set('categoryId', c.id)
          setSheetOpen(false)
        }}
      />
    </form>
  )
}
