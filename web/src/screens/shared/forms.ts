import type { ApiError } from '../../api/errors'
import { useNav } from '../../state/nav'
import { useBackOverride, useClosingGuard, useTelegram } from '../../telegram/hooks'

export type FieldErrors<K extends string> = Partial<Record<K | 'form', string>>

/** Maps the server's error.field onto a form field, or onto the form as a whole. */
export function fieldOf<K extends string>(field: string | undefined, known: readonly K[]): K | 'form' {
  return field !== undefined && (known as readonly string[]).includes(field) ? (field as K) : 'form'
}

/** Copy for an upload the server refused or that never arrived. */
export function uploadFailureText(errors: readonly ApiError[], maxBytes: number): string {
  const mb = Math.max(1, Math.round(maxBytes / (1024 * 1024)))
  const limit = errors.find((e) => e.code === 'limit')
  if (limit) return limit.message
  if (errors.every((e) => e.isRetryable)) return 'Не все фото загрузились — проблемы со связью. Нажмите «Сохранить» ещё раз.'
  return `Не получилось загрузить фото. Поддерживаются JPEG, PNG, WebP до ${mb} МБ.`
}

/**
 * Protects unsaved input: Telegram asks before closing the app, swipe-to-close
 * is off, and the BackButton asks before leaving the form. While a save is in
 * flight Back is ignored, so the save cannot finish on top of another screen.
 */
export function useLeaveGuard(dirty: boolean, saving: boolean): void {
  const tg = useTelegram()
  const nav = useNav()
  useClosingGuard(dirty || saving)
  useBackOverride(() => {
    if (saving) {
      tg.haptic.notify('warning')
      return
    }
    void tg.confirm('Выйти без сохранения? Изменения пропадут.', 'Выйти').then((leave) => {
      if (leave) nav.pop()
    })
  }, dirty || saving)
}

/**
 * Enter/"Done" in a text input only hides the keyboard: saving stays on the
 * MainButton, so dismissing the keyboard never submits by accident.
 */
export function dismissKeyboard(tg: { hideKeyboard(): void }) {
  return (e: { preventDefault(): void }) => {
    e.preventDefault()
    tg.hideKeyboard()
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur()
  }
}
