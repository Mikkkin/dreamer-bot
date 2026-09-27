import { createContext, use, useCallback, useEffect, useState, type ReactNode } from 'react'
import { IconAlert, IconCheck } from '../ui/icons'

export type ToastTone = 'success' | 'error' | 'info'
type ShowToast = (text: string, options?: { tone?: ToastTone }) => void

const ToastContext = createContext<ShowToast>(() => {})

export function useToast(): ShowToast {
  return use(ToastContext)
}

const TOAST_MS = 2000
const LEAVE_MS = 200

interface ToastState {
  id: number
  text: string
  tone: ToastTone
  leaving: boolean
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<ToastState | null>(null)

  useEffect(() => {
    if (!toast) return
    const timer = toast.leaving
      ? window.setTimeout(() => setToast((t) => (t?.id === toast.id ? null : t)), LEAVE_MS)
      : window.setTimeout(() => setToast((t) => (t?.id === toast.id ? { ...t, leaving: true } : t)), TOAST_MS)
    return () => window.clearTimeout(timer)
  }, [toast])

  const show = useCallback<ShowToast>(
    (text, options) => setToast((t) => ({ id: (t?.id ?? 0) + 1, text, tone: options?.tone ?? 'info', leaving: false })),
    [],
  )

  return (
    <ToastContext value={show}>
      {children}
      <div className="toast-region" role="status" aria-live="polite">
        {toast && (
          <div key={toast.id} className={`toast toast--${toast.tone}`} data-leaving={toast.leaving || undefined}>
            {toast.tone === 'success' && (
              <span className="toast__icon" aria-hidden="true">
                <IconCheck size={14} strokeWidth={3} />
              </span>
            )}
            {toast.tone === 'error' && (
              <span className="toast__icon" aria-hidden="true">
                <IconAlert size={18} strokeWidth={2.2} />
              </span>
            )}
            <span className="toast__text">{toast.text}</span>
          </div>
        )}
      </div>
    </ToastContext>
  )
}
