import { useId, type ChangeEvent } from 'react'
import { cx } from '../lib/cx'
import type { PhotoDraft, PhotoTile } from '../media/usePhotoDraft'
import { useToast } from '../state/toast'
import { useTelegram } from '../telegram/hooks'
import { IconImagePlus, IconPlus, IconX } from './icons'

const ACCEPT = 'image/jpeg,image/png,image/webp'
const RING_R = 16
const RING_C = 2 * Math.PI * RING_R

interface PhotoEditorProps {
  label: string
  draft: PhotoDraft
  max: number
  alt: string
  /** The drop zone's title and hint while there are no photos yet. */
  emptyTitle?: string
  emptyHint?: string
  disabled?: boolean
}

/**
 * Photos of a form in their own surface: a full-width drop zone while empty,
 * then a 3-column grid with an "add" tile. The first photo is the cover.
 */
export function PhotoEditor({ label, draft, max, alt, emptyTitle = 'Добавить фото', emptyHint, disabled }: PhotoEditorProps) {
  const tg = useTelegram()
  const toast = useToast()
  const inputId = useId()
  const headerId = useId()

  const onPick = async (e: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? [])
    e.target.value = ''
    if (files.length === 0) return
    tg.haptic.impact('light')
    const { skipped } = await draft.add(files)
    if (skipped > 0) toast(`Можно добавить не больше ${max} фото`, { tone: 'error' })
  }

  const canAdd = draft.room > 0 && !disabled
  const empty = draft.tiles.length === 0 && draft.preparing === 0
  const input = (
    <input id={inputId} className="visually-hidden" type="file" accept={ACCEPT} multiple onChange={(e) => void onPick(e)} />
  )

  return (
    <section className="section photos-section" aria-labelledby={headerId}>
      <h2 className="section__header" id={headerId}>
        {label}
        {!empty && (
          <span className="photos__count num">
            {draft.tiles.length}/{max}
          </span>
        )}
      </h2>
      <div className="photos">
        {empty && canAdd ? (
          <label className="photo-dropzone" htmlFor={inputId}>
            <span className="photo-dropzone__icon" aria-hidden="true">
              <IconImagePlus size={22} />
            </span>
            <span className="photo-dropzone__text">
              <span className="photo-dropzone__title">{emptyTitle}</span>
              <span className="photo-dropzone__hint">{emptyHint ?? `До ${max} фото · первое станет обложкой`}</span>
            </span>
            {input}
          </label>
        ) : (
          <div className="photos__grid">
            {draft.tiles.map((tile, i) => (
              <Tile
                key={tile.key}
                tile={tile}
                cover={i === 0}
                alt={`${alt} — фото ${i + 1}`}
                disabled={disabled}
                onRemove={() => {
                  tg.haptic.impact('light')
                  draft.remove(tile.key)
                }}
              />
            ))}
            {Array.from({ length: draft.preparing }, (_, i) => (
              <div key={`prep-${i}`} className="photo-tile photo-tile--preparing" aria-label="Обрабатываем фото…" role="img">
                <span className="spinner" aria-hidden="true" />
              </div>
            ))}
            {canAdd && (
              <label className="photo-tile photo-tile--add" htmlFor={inputId}>
                <IconPlus size={22} />
                <span>Ещё</span>
                {input}
              </label>
            )}
          </div>
        )}
      </div>
    </section>
  )
}

function Tile({
  tile,
  cover,
  alt,
  disabled,
  onRemove,
}: {
  tile: PhotoTile
  cover: boolean
  alt: string
  disabled?: boolean
  onRemove: () => void
}) {
  const src = tile.kind === 'saved' ? tile.image.thumb_url : tile.preview
  const progress = tile.kind === 'new' ? tile.progress : null
  const failed = tile.kind === 'new' && tile.failed
  return (
    <div className={cx('photo-tile', failed && 'photo-tile--failed')}>
      <img src={src} alt={alt} decoding="async" draggable={false} />
      {cover && <span className="photo-tile__cover">Обложка</span>}
      {progress !== null && (
        <span className="photo-tile__progress" role="progressbar" aria-valuenow={Math.round(progress * 100)} aria-valuemin={0} aria-valuemax={100}>
          <svg viewBox="0 0 40 40" aria-hidden="true">
            <circle className="ring-track" cx="20" cy="20" r={RING_R} />
            <circle
              className="ring-value"
              cx="20"
              cy="20"
              r={RING_R}
              strokeDasharray={RING_C}
              strokeDashoffset={RING_C * (1 - progress)}
            />
          </svg>
        </span>
      )}
      {failed && (
        <span className="photo-tile__failed" role="img" aria-label="Не загрузилось">
          !
        </span>
      )}
      {!disabled && (
        <button type="button" className="photo-tile__remove" aria-label="Убрать фото" onClick={onRemove}>
          <span aria-hidden="true">
            <IconX size={14} strokeWidth={2.6} />
          </span>
        </button>
      )}
    </div>
  )
}
