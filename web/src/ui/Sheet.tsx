import { useEffect, useEffectEvent, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useBackOverride } from '../telegram/hooks'

interface SheetProps {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
}

/** A bottom sheet. The BackButton and a backdrop tap close it; it stays mounted while animating out. */
export function Sheet({ open, onClose, title, children }: SheetProps) {
  const [mounted, setMounted] = useState(open)
  if (open && !mounted) setMounted(true)
  const panel = useRef<HTMLDivElement>(null)

  useBackOverride(onClose, open)
  const onEscape = useEffectEvent(onClose)

  useEffect(() => {
    if (!open) return
    const { overflow } = document.body.style
    document.body.style.overflow = 'hidden'
    panel.current?.focus({ preventScroll: true })
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onEscape()
    }
    window.addEventListener('keydown', onKey)
    return () => {
      document.body.style.overflow = overflow
      window.removeEventListener('keydown', onKey)
    }
  }, [open])

  if (!mounted) return null
  return createPortal(
    <div className="sheet" data-open={open}>
      <div className="sheet__backdrop" onClick={onClose} aria-hidden="true" />
      <div
        ref={panel}
        className="sheet__panel"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        onAnimationEnd={(e) => {
          if (!open && e.target === e.currentTarget) setMounted(false)
        }}
      >
        <div className="sheet__grabber" aria-hidden="true" />
        <h2 className="sheet__title">{title}</h2>
        <div className="sheet__content">{children}</div>
      </div>
    </div>,
    document.body,
  )
}
