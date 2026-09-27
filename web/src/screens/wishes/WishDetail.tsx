import { useMemo, useState } from 'react'
import { ApiError } from '../../api/errors'
import type { Status, Wish } from '../../api/types'
import { categoryBackdrop, categoryEmoji, categoryGradient } from '../../lib/categoryStyle'
import { celebrate } from '../../lib/confetti'
import { formatByline, formatDay } from '../../lib/format'
import { glueShortWords } from '../../lib/typo'
import { personSlot, useData, useMe } from '../../state/data'
import { useNav } from '../../state/nav'
import { useStillOnTop } from '../../state/screen'
import { useToast } from '../../state/toast'
import { useMainButton, useSecondaryButton, useTelegram } from '../../telegram/hooks'
import { Backdrop } from '../../ui/art'
import { StatusStepper } from '../../ui/controls'
import { IconPencil } from '../../ui/icons'
import { Cell, IconTile, Section } from '../../ui/layout'
import { Avatar, Carousel } from '../../ui/media'
import { LinkCell } from '../shared/LinkCell'
import { LoadState } from '../shared/LoadState'
import { useEntity } from '../shared/useEntity'
import { categoryMap, categoryOf } from './model'

export function WishDetail({ id }: { id: number }) {
  const data = useData()
  const me = useMe()
  const nav = useNav()
  const tg = useTelegram()
  const toast = useToast()
  const lookup = useEntity(data.wishes, id, (wishId) => data.api.wish(wishId))
  const [busy, setBusy] = useState(false)
  const stillOnTop = useStillOnTop()
  const wish = lookup.entity

  const byId = useMemo(() => categoryMap(data.categories), [data.categories])

  const changeStatus = async (current: Wish, status: Status) => {
    if (busy || current.status === status) return
    setBusy(true)
    try {
      const updated = await data.api.setWishStatus(current.id, status)
      data.putWish(updated)
      if (status === 'done') {
        tg.haptic.notify('success')
        window.setTimeout(() => tg.haptic.impact('heavy'), 120)
        celebrate()
      } else {
        tg.haptic.impact('light')
      }
    } catch (err) {
      tg.haptic.notify('error')
      if (err instanceof ApiError && !err.isAuth) toast(err.message, { tone: 'error' })
    } finally {
      setBusy(false)
    }
  }

  const edit = () => nav.push({ name: 'wish-form', id })

  useMainButton(
    wish
      ? {
          text: wish.status === 'done' ? 'Вернуть в список' : 'Сбылось ✨',
          shine: wish.status !== 'done',
          progress: busy,
          onClick: () => void changeStatus(wish, wish.status === 'done' ? 'want' : 'done'),
        }
      : null,
  )
  const hasSecondary = useSecondaryButton(wish ? { text: 'Изменить', onClick: edit } : null)

  if (!wish) return <LoadState lookup={lookup} missingTitle="Мечта не найдена" missingText="Возможно, её уже удалили." />

  const category = categoryOf(wish, byId)
  const backdrop = categoryBackdrop(category)
  const done = wish.status === 'done'

  const remove = async () => {
    tg.haptic.notify('warning')
    if (!(await tg.confirm(`Удалить «${wish.title}»?`, 'Удалить'))) return
    try {
      await data.api.deleteWish(wish.id)
      if (stillOnTop()) nav.pop()
      data.dropWish(wish.id)
      toast('Удалено', { tone: 'success' })
    } catch (err) {
      tg.haptic.notify('error')
      if (err instanceof ApiError && !err.isAuth) toast(err.message, { tone: 'error' })
    }
  }

  return (
    <article className="detail">
      <Carousel
        images={wish.images}
        alt={wish.title}
        background={categoryGradient(category)}
        glow={backdrop.edge}
        onOpen={(start) => nav.push({ name: 'viewer', images: wish.images, start, title: wish.title })}
        fallback={<Backdrop emoji={categoryEmoji(category)} {...backdrop} size="hero" seed={wish.id} />}
      />

      <header className="detail__head">
        <p className="detail__overline">
          <span className="detail__overline-emoji" aria-hidden="true">
            {categoryEmoji(category)}
          </span>
          {category ? category.name : 'Без категории'}
        </p>
        <h1 className="detail__title">{glueShortWords(wish.title)}</h1>
        <div className="detail__byline">
          <Avatar name={wish.author.name} slot={personSlot(me, wish.author.id)} />
          <span>{formatByline(wish.author.name, wish.created_at)}</span>
          {done && wish.fulfilled_at && <span className="detail__done">· сбылось {formatDay(wish.fulfilled_at)} ✨</span>}
          {wish.hot && !done && <span className="mini-tag mini-tag--hot">🔥 Очень хочется</span>}
        </div>
        {wish.price && <p className={done ? 'detail__price detail__price--past num' : 'detail__price num'}>{wish.price.formatted}</p>}
      </header>

      {wish.link && <LinkCell url={wish.link} title="Открыть ссылку" />}

      {wish.note && (
        <Section header="Заметка">
          <p className="prose">{wish.note}</p>
        </Section>
      )}

      <Section header="Путь мечты">
        <StatusStepper value={wish.status} busy={busy} onChange={(s) => void changeStatus(wish, s)} />
      </Section>

      <Section className="section--actions">
        {!hasSecondary && (
          <Cell
            before={
              <IconTile>
                <IconPencil size={18} />
              </IconTile>
            }
            title="Изменить"
            chevron
            onClick={edit}
          />
        )}
        <Cell tone="destructive" title="Удалить желание" onClick={() => void remove()} />
      </Section>
    </article>
  )
}
