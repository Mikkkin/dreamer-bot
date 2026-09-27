import type { CSSProperties, ReactNode } from 'react'
import type { ApiError } from '../api/errors'
import { cx } from '../lib/cx'
import { Sparkles, Sticker } from './art'
import { IconAlert, IconChevron } from './icons'

export function Section({
  header,
  footer,
  className,
  children,
}: {
  header?: ReactNode
  footer?: ReactNode
  className?: string
  children: ReactNode
}) {
  return (
    <section className={cx('section', className)}>
      {header && <h2 className="section__header">{header}</h2>}
      <div className="section__body">{children}</div>
      {footer && <p className="section__footer">{footer}</p>}
    </section>
  )
}

interface CellProps {
  before?: ReactNode
  title: ReactNode
  subtitle?: ReactNode
  after?: ReactNode
  /** true draws the chevron; a node replaces it (e.g. an "opens outside" arrow). */
  chevron?: boolean | ReactNode
  tone?: 'default' | 'accent' | 'destructive'
  onClick?: () => void
  disabled?: boolean
}

/** A row inside a Section; a button when it has an action. */
export function Cell({ before, title, subtitle, after, chevron, tone = 'default', onClick, disabled }: CellProps) {
  const content = (
    <>
      {before && <span className="cell__before">{before}</span>}
      <span className="cell__main">
        <span className="cell__title">{title}</span>
        {subtitle && <span className="cell__subtitle">{subtitle}</span>}
      </span>
      {after !== undefined && <span className="cell__after">{after}</span>}
      {chevron && (
        <span className="cell__chevron" aria-hidden="true">
          {chevron === true ? <IconChevron size={18} strokeWidth={2.2} /> : chevron}
        </span>
      )}
    </>
  )
  const className = cx('cell', tone !== 'default' && `cell--${tone}`)
  return onClick ? (
    <button type="button" className={className} onClick={onClick} disabled={disabled}>
      {content}
    </button>
  ) : (
    <div className={className}>{content}</div>
  )
}

/** A rounded icon tile before a cell title, as in iOS Settings. */
export function IconTile({
  tone = 'accent',
  children,
  style,
}: {
  tone?: 'accent' | 'green' | 'orange' | 'rose' | 'amber' | 'violet'
  children: ReactNode
  style?: CSSProperties
}) {
  return (
    <span className={cx('icon-tile', `icon-tile--${tone}`)} style={style} aria-hidden="true">
      {children}
    </span>
  )
}

export function EmptyState({
  emoji,
  title,
  text,
  children,
}: {
  emoji: string
  title: string
  text?: ReactNode
  children?: ReactNode
}) {
  return (
    <div className="empty">
      <div className="empty__art" aria-hidden="true">
        <Sticker emoji={emoji} size={64} tilt={-6} className="empty__sticker" />
        <Sparkles />
      </div>
      <h2 className="empty__title">{title}</h2>
      {text && <p className="empty__text">{text}</p>}
      {children}
    </div>
  )
}

export function RetryBanner({ error, onRetry }: { error: ApiError; onRetry: () => void }) {
  return (
    <div className="banner" role="alert">
      <span className="banner__icon" aria-hidden="true">
        <IconAlert size={20} />
      </span>
      <span className="banner__text">{error.code === 'network' ? 'Нет соединения' : error.message}</span>
      <button type="button" className="banner__action" onClick={onRetry}>
        Повторить
      </button>
    </div>
  )
}

export function IconButton({ label, onClick, children }: { label: string; onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" className="icon-btn" aria-label={label} title={label} onClick={onClick}>
      {children}
    </button>
  )
}

export function Skeleton({
  width,
  height,
  radius,
  className,
  delay,
}: {
  width?: string
  height?: string
  radius?: string
  className?: string
  /** Staggers the shimmer so it flows across a group. */
  delay?: number
}) {
  const style: CSSProperties = { width, height, borderRadius: radius, animationDelay: delay ? `${delay}ms` : undefined }
  return <span className={cx('skeleton', className)} style={style} aria-hidden="true" />
}

/** An in-page action for clients without a native SecondaryButton. */
export function InlineAction({
  children,
  icon,
  onClick,
  disabled,
}: {
  children: ReactNode
  icon?: ReactNode
  onClick: () => void
  disabled?: boolean
}) {
  return (
    <button type="button" className="inline-action" onClick={onClick} disabled={disabled}>
      {icon && <span className="inline-action__icon">{icon}</span>}
      {children}
    </button>
  )
}
