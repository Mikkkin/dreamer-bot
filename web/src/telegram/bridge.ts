import type { BackButton, BottomButton, WebApp } from 'telegram-web-app'

// A thin adapter over the official telegram-web-app.js: version guards, safe
// fallbacks for a plain browser, and arbitration of the singleton native
// buttons between screens and sheets.

export type ImpactStyle = 'light' | 'medium' | 'heavy' | 'rigid' | 'soft'
export type NotificationType = 'error' | 'success' | 'warning'

export interface ButtonSpec {
  text: string
  onClick: () => void
  active?: boolean
  progress?: boolean
  shine?: boolean
}

type Listener = () => void

/**
 * Shares one native bottom button: the most recent claimant is shown, and
 * releasing a claim restores the previous one (a sheet over a screen). Hiding
 * is deferred by a microtask so that swapping screens within one React commit
 * never flashes the button.
 */
export class BottomButtonController {
  private claims: { owner: symbol; spec: ButtonSpec }[] = []

  constructor(
    private readonly button: BottomButton,
    readonly supported: boolean,
    private readonly position?: 'left' | 'right' | 'top' | 'bottom',
  ) {
    if (supported) button.onClick(() => this.claims.at(-1)?.spec.onClick())
  }

  set(owner: symbol, spec: ButtonSpec): void {
    const claim = this.claims.find((c) => c.owner === owner)
    if (claim) claim.spec = spec
    else this.claims.push({ owner, spec })
    this.render()
  }

  release(owner: symbol): void {
    const before = this.claims.length
    this.claims = this.claims.filter((c) => c.owner !== owner)
    if (this.claims.length !== before) queueMicrotask(() => this.render())
  }

  private render(): void {
    if (!this.supported) return
    const spec = this.claims.at(-1)?.spec
    try {
      if (!spec) {
        this.button.hideProgress()
        this.button.hide()
        return
      }
      const progress = spec.progress ?? false
      // hideProgress() re-activates the button, so it must run before setParams.
      if (progress) this.button.showProgress(false)
      else this.button.hideProgress()
      this.button.setParams({
        text: spec.text,
        is_visible: true,
        is_active: (spec.active ?? true) && !progress,
        has_shine_effect: spec.shine ?? false,
        ...(this.position ? { position: this.position } : {}),
      })
    } catch (err) {
      console.warn('bottom button update failed', err)
    }
  }
}

/**
 * The native BackButton: the navigation stack provides the base handler, and
 * sheets or dirty forms push overrides that take precedence while mounted.
 */
export class BackButtonController {
  private base: { handler: () => void; visible: boolean } = { handler: () => {}, visible: false }
  private overrides: { owner: symbol; handler: () => void }[] = []
  private listeners = new Set<Listener>()
  private shown = false

  constructor(
    private readonly button: BackButton,
    private readonly supported: boolean,
  ) {
    if (supported) button.onClick(() => this.trigger())
  }

  setBase(handler: () => void, visible: boolean): void {
    this.base = { handler, visible }
    this.sync()
  }

  push(owner: symbol, handler: () => void): void {
    this.overrides = [...this.overrides.filter((o) => o.owner !== owner), { owner, handler }]
    this.sync()
  }

  remove(owner: symbol): void {
    this.overrides = this.overrides.filter((o) => o.owner !== owner)
    queueMicrotask(() => this.sync())
  }

  trigger(): void {
    const top = this.overrides.at(-1)
    if (top) top.handler()
    else if (this.base.visible) this.base.handler()
  }

  get visible(): boolean {
    return this.shown
  }

  subscribe = (listener: Listener): (() => void) => {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  private sync(): void {
    const visible = this.base.visible || this.overrides.length > 0
    if (visible === this.shown) return
    this.shown = visible
    if (this.supported) {
      if (visible) this.button.show()
      else this.button.hide()
    }
    for (const l of this.listeners) l()
  }
}

export class TelegramBridge {
  readonly main: BottomButtonController
  readonly secondary: BottomButtonController
  readonly back: BackButtonController
  /** True when a real Telegram client is on the other side (not a plain browser tab). */
  readonly native: boolean
  private popupOpen = false

  constructor(private readonly app: WebApp) {
    this.native = detectNativeBridge()
    this.main = new BottomButtonController(app.MainButton, true)
    this.secondary = new BottomButtonController(app.SecondaryButton, this.supports('7.10'), 'left')
    this.back = new BackButtonController(app.BackButton, this.supports('6.1'))
  }

  /** The raw, signed launch data; the server validates it on every request. */
  get initData(): string {
    return this.app.initData
  }

  get startParam(): string | undefined {
    return this.app.initDataUnsafe.start_param
  }

  supports(version: string): boolean {
    try {
      return this.app.isVersionAtLeast(version)
    } catch {
      return false
    }
  }

  /** Applies theme and platform hints to <html> and sizes the WebView. Call once before the first render. */
  boot(): void {
    const root = document.documentElement
    const applyScheme = () => {
      // Without a theme the SDK reports "light"; leave prefers-color-scheme in charge then.
      if (this.app.themeParams.bg_color) root.dataset.scheme = this.app.colorScheme
    }
    applyScheme()
    this.app.onEvent('themeChanged', applyScheme)
    root.dataset.platform = this.app.platform
    if (/; LOW\)\s*$/.test(navigator.userAgent)) root.dataset.perf = 'low'

    this.safely(() => this.app.expand())
    if (this.supports('6.1')) {
      this.safely(() => this.app.setHeaderColor('secondary_bg_color'))
      this.safely(() => this.app.setBackgroundColor('secondary_bg_color'))
    }
    if (this.supports('7.10')) this.safely(() => this.app.setBottomBarColor('secondary_bg_color'))
  }

  /**
   * Paints Telegram's header, background and bottom bar in one colour (the
   * photo viewer goes black); null restores the theme's page colour.
   */
  setChromeColor(color: `#${string}` | null): void {
    if (!this.supports('6.9')) return
    const key = color ?? 'secondary_bg_color'
    this.safely(() => this.app.setHeaderColor(key))
    this.safely(() => this.app.setBackgroundColor(key))
    if (this.supports('7.10')) this.safely(() => this.app.setBottomBarColor(key))
  }

  /** Hides Telegram's loading placeholder. */
  ready(): void {
    this.safely(() => this.app.ready())
  }

  close(): void {
    this.safely(() => this.app.close())
  }

  hideKeyboard(): void {
    if (this.supports('9.1')) this.safely(() => this.app.hideKeyboard())
  }

  readonly haptic = {
    selection: () => this.withHaptics((h) => h.selectionChanged()),
    impact: (style: ImpactStyle) => this.withHaptics((h) => h.impactOccurred(style)),
    notify: (type: NotificationType) => this.withHaptics((h) => h.notificationOccurred(type)),
  }

  /** Opens an external http(s) link in the Telegram in-app browser; the Mini App stays open. */
  openLink(url: string): void {
    this.safely(() => this.app.openLink(url))
  }

  /** A native destructive confirmation; window.confirm in a plain browser, where popups never resolve. */
  confirm(message: string, okText: string): Promise<boolean> {
    if (!this.canPopup()) return Promise.resolve(window.confirm(message))
    return new Promise((resolve) => {
      this.popupOpen = true
      try {
        this.app.showPopup(
          { message, buttons: [{ id: 'ok', type: 'destructive', text: okText }, { type: 'cancel' }] },
          (id) => {
            this.popupOpen = false
            resolve(id === 'ok')
          },
        )
      } catch {
        this.popupOpen = false
        resolve(window.confirm(message))
      }
    })
  }

  alert(message: string): Promise<void> {
    if (!this.canPopup()) {
      window.alert(message)
      return Promise.resolve()
    }
    return new Promise((resolve) => {
      this.popupOpen = true
      try {
        this.app.showAlert(message, () => {
          this.popupOpen = false
          resolve()
        })
      } catch {
        this.popupOpen = false
        resolve()
      }
    })
  }

  /** Protects unsaved input: asks before closing and stops swipe-to-close. Returns the undo. */
  guardClosing(): () => void {
    this.safely(() => this.app.enableClosingConfirmation())
    const swipes = this.supports('7.7')
    if (swipes) this.safely(() => this.app.disableVerticalSwipes())
    return () => {
      this.safely(() => this.app.disableClosingConfirmation())
      if (swipes) this.safely(() => this.app.enableVerticalSwipes())
    }
  }

  /** Shows the "Settings" item in the ⋯ menu. Returns the unsubscribe. */
  onSettings(handler: () => void): () => void {
    if (!this.supports('6.10')) return () => {}
    const button = this.app.SettingsButton
    this.safely(() => button.onClick(handler).show())
    return () => this.safely(() => button.offClick(handler).hide())
  }

  /** Fires when the Mini App becomes visible again (8.0+), or the tab regains focus. */
  onActivated(handler: () => void): () => void {
    const onVisible = () => {
      if (document.visibilityState === 'visible') handler()
    }
    document.addEventListener('visibilitychange', onVisible)
    const native = this.supports('8.0')
    if (native) this.app.onEvent('activated', handler)
    return () => {
      document.removeEventListener('visibilitychange', onVisible)
      if (native) this.app.offEvent('activated', handler)
    }
  }

  private canPopup(): boolean {
    return this.native && this.supports('6.2') && !this.popupOpen
  }

  private withHaptics(fn: (h: WebApp['HapticFeedback']) => void): void {
    if (this.native && this.supports('6.1')) this.safely(() => fn(this.app.HapticFeedback))
  }

  private safely(fn: () => void): void {
    try {
      fn()
    } catch (err) {
      console.warn('Telegram WebApp call failed', err)
    }
  }
}

function detectNativeBridge(): boolean {
  const w = window as unknown as {
    TelegramWebviewProxy?: unknown
    external?: object
    Telegram?: { WebView?: { isIframe?: boolean } }
  }
  return (
    w.TelegramWebviewProxy !== undefined ||
    Boolean(w.Telegram?.WebView?.isIframe) ||
    (typeof w.external === 'object' && w.external !== null && 'notify' in w.external)
  )
}

/** Returns null when telegram-web-app.js failed to load (blocked network, opened outside Telegram). */
export function createTelegram(): TelegramBridge | null {
  const app = (window as unknown as { Telegram?: { WebApp?: WebApp } }).Telegram?.WebApp
  return app ? new TelegramBridge(app) : null
}
