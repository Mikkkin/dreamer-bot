import { createContext, use, useEffect, useEffectEvent, useState, useSyncExternalStore } from 'react'
import { useIsTopScreen } from '../state/screen'
import type { BottomButtonController, ButtonSpec, TelegramBridge } from './bridge'

export const TelegramContext = createContext<TelegramBridge | null>(null)

export function useTelegram(): TelegramBridge {
  const tg = use(TelegramContext)
  if (!tg) throw new Error('useTelegram must be used inside TelegramContext')
  return tg
}

/**
 * Binds a native bottom button while the calling screen is on top. Pass null
 * to leave the button to someone else (it hides when nobody claims it).
 */
function useBottomButton(controller: BottomButtonController, config: ButtonSpec | null): void {
  const isTop = useIsTopScreen()
  const [owner] = useState(() => Symbol('bottom-button'))
  const onClick = useEffectEvent(() => config?.onClick())
  const live = isTop && config !== null
  const text = config?.text ?? ''
  const active = config?.active ?? true
  const progress = config?.progress ?? false
  const shine = config?.shine ?? false

  useEffect(() => {
    if (!live) return
    controller.set(owner, { text, active, progress, shine, onClick: () => onClick() })
  }, [controller, owner, live, text, active, progress, shine])

  useEffect(() => {
    if (!live) return
    return () => controller.release(owner)
  }, [controller, owner, live])
}

export function useMainButton(config: ButtonSpec | null): void {
  useBottomButton(useTelegram().main, config)
}

/** Returns false when the client has no SecondaryButton (Bot API < 7.10); render an in-page action then. */
export function useSecondaryButton(config: ButtonSpec | null): boolean {
  const tg = useTelegram()
  useBottomButton(tg.secondary, config)
  return tg.secondary.supported
}

/** Takes over the BackButton (a sheet, a form with unsaved input) while enabled and on top. */
export function useBackOverride(handler: () => void, enabled: boolean): void {
  const tg = useTelegram()
  const isTop = useIsTopScreen()
  const [owner] = useState(() => Symbol('back'))
  const onBack = useEffectEvent(handler)
  const live = enabled && isTop

  useEffect(() => {
    if (!live) return
    tg.back.push(owner, () => onBack())
    return () => tg.back.remove(owner)
  }, [tg, owner, live])
}

export function useBackVisible(): boolean {
  const tg = useTelegram()
  return useSyncExternalStore(tg.back.subscribe, () => tg.back.visible)
}

/** Enables Telegram's closing confirmation and disables swipe-to-close while active. */
export function useClosingGuard(active: boolean): void {
  const tg = useTelegram()
  useEffect(() => (active ? tg.guardClosing() : undefined), [tg, active])
}
