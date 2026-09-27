import type { CSSProperties } from 'react'
import type { Category, Wish } from '../../api/types'
import { categoryBackdrop, categoryEmoji } from '../../lib/categoryStyle'
import { cx } from '../../lib/cx'
import { formatShortDay } from '../../lib/format'
import { glueShortWords } from '../../lib/typo'
import { Backdrop } from '../../ui/art'
import { IconPiggy } from '../../ui/icons'
import { Avatar, Img } from '../../ui/media'

interface WishCardProps {
  wish: Wish
  /** Position in the grid, for the staggered entrance. */
  index: number
  category: Category | undefined
  /** The list is not filtered by category, so the card names its own. */
  showCategory: boolean
  authorSlot: 'a' | 'b'
  onOpen: () => void
}

/** A framed card: the cover sits inside the card with a concentric radius. */
export function WishCard({ wish, index, category, showCategory, authorSlot, onOpen }: WishCardProps) {
  const cover = wish.images[0]
  const done = wish.status === 'done'
  const progress = wish.status === 'progress'
  const backdrop = categoryBackdrop(category)
  const style = { '--i': Math.min(index, 8), '--bd-c': backdrop.center, '--bd-e': backdrop.edge } as CSSProperties
  return (
    <button type="button" className={cx('card', done && 'card--done')} style={style} onClick={onOpen}>
      <span className="card__cover">
        {cover ? (
          <Img src={cover.thumb_url} alt={wish.title} />
        ) : (
          <Backdrop emoji={categoryEmoji(category)} {...backdrop} seed={wish.id} />
        )}
        {cover && showCategory && (
          <span className="badge badge--tl" role="img" aria-label={category?.name ?? 'Без категории'}>
            {categoryEmoji(category)}
          </span>
        )}
        {!done && wish.hot && (
          <span className="badge badge--tr" role="img" aria-label="Очень хочется">
            🔥
          </span>
        )}
        {done && (
          <span className="stamp" aria-label={wish.fulfilled_at ? `Сбылось ${formatShortDay(wish.fulfilled_at)}` : 'Сбылось'}>
            <span aria-hidden="true">✨</span>
            <span aria-hidden="true">{wish.fulfilled_at ? formatShortDay(wish.fulfilled_at) : 'Сбылось'}</span>
          </span>
        )}
      </span>
      <span className="card__body">
        <span className="card__title">{glueShortWords(wish.title)}</span>
        {progress && (
          <span className="mini-tag mini-tag--progress">
            <IconPiggy size={14} strokeWidth={2.2} />
            Копим
          </span>
        )}
        <span className="card__meta">
          {wish.price ? (
            <span className={cx('card__price num', done && 'card__price--past')}>{wish.price.formatted}</span>
          ) : showCategory ? (
            <span className="card__category">{category ? category.name : 'Без категории'}</span>
          ) : (
            <span />
          )}
          <Avatar name={wish.author.name} slot={authorSlot} />
        </span>
      </span>
    </button>
  )
}
