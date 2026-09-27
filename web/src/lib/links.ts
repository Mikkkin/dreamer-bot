// User links are shown as text and opened only through Telegram.WebApp.openLink
// after this check; they are never rendered as a live <a href>.

const HTTP_SCHEME_RE = /^https?:\/\//i
const MAX_LINK_LEN = 2048

/** True for absolute http(s) URLs with a host and without embedded credentials. */
export function isSafeHttpUrl(raw: string): boolean {
  if (!HTTP_SCHEME_RE.test(raw)) return false
  try {
    const u = new URL(raw)
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.hostname !== '' && u.username === '' && u.password === ''
  } catch {
    return false
  }
}

export type LinkResult = { ok: true; url: string } | { ok: false; message: string }

/**
 * Mirrors the server's NormalizeLink: trims, upgrades a bare domain such as
 * "ozon.ru/item" to https, and accepts only http(s) with a host.
 */
export function normalizeLinkInput(raw: string, maxLen = MAX_LINK_LEN): LinkResult {
  let s = raw.trim()
  if (s === '') return { ok: false, message: 'Вставьте ссылку' }
  if (s.length > maxLen) return { ok: false, message: 'Ссылка слишком длинная' }
  if (!s.includes('://')) s = `https://${s}`
  if (!HTTP_SCHEME_RE.test(s)) return { ok: false, message: 'Поддерживаются только ссылки http и https' }
  let u: URL
  try {
    u = new URL(s)
  } catch {
    return { ok: false, message: 'Это не похоже на ссылку' }
  }
  if (u.hostname === '' || /\s/.test(u.host)) return { ok: false, message: 'В ссылке нет адреса сайта' }
  if (u.username !== '' || u.password !== '') return { ok: false, message: 'Ссылки с логином и паролем не поддерживаются' }
  if (!isSafeHttpUrl(u.href)) return { ok: false, message: 'Это не похоже на ссылку' }
  return { ok: true, url: s }
}

/** The host to show next to a link ("www." dropped). Punycode stays as-is on purpose: it exposes homographs. */
export function linkHost(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./i, '')
  } catch {
    return ''
  }
}
