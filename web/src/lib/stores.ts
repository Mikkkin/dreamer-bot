import type { Store } from '../api/types'
import { isSafeHttpUrl } from './links'

// Store search deep links. The templates come from the server, but the client
// still refuses anything that is not https with a {q} slot on a known store
// host, encodes the query as one URI component, and opens the result only via
// Telegram.WebApp.openLink.

const PLACEHOLDER = '{q}'
const HTTPS_RE = /^https:\/\//i

/** The hosts of internal/stores; a template elsewhere is never opened. */
export const STORE_HOSTS: ReadonlySet<string> = new Set([
  'vkusvill.ru',
  'www.perekrestok.ru',
  'magnit.ru',
  '5ka.ru',
  'lavka.yandex.ru',
  'samokat.ru',
  'kuper.ru',
  'lenta.com',
  'www.auchan.ru',
  'online.metro-cc.ru',
  'av.ru',
])

/** True for an https template with a {q} slot and a fixed, known store host. */
export function isValidStoreTemplate(template: string): boolean {
  if (!HTTPS_RE.test(template) || !template.includes(PLACEHOLDER)) return false
  try {
    const probe = new URL(template.split(PLACEHOLDER).join('x'))
    const bare = new URL(template.split(PLACEHOLDER).join(''))
    // The slot must never be part of the host.
    return (
      probe.protocol === 'https:' &&
      probe.host === bare.host &&
      STORE_HOSTS.has(probe.host) &&
      isSafeHttpUrl(probe.href)
    )
  } catch {
    return false
  }
}

/** The store's search URL for a product name, or null when it cannot be built safely. */
export function storeSearchUrl(template: string, query: string): string | null {
  const q = query.replace(/\s+/gu, ' ').trim()
  if (q === '' || !isValidStoreTemplate(template)) return null
  const url = template.split(PLACEHOLDER).join(encodeURIComponent(q))
  if (!isSafeHttpUrl(url)) return null
  try {
    if (new URL(url).host !== new URL(template.split(PLACEHOLDER).join('')).host) return null
  } catch {
    return null
  }
  return url
}

/** Only stores whose template passes the check are offered. */
export function usableStores(stores: readonly Store[]): Store[] {
  return stores.filter((s) => isValidStoreTemplate(s.search_url_template))
}

/** The remembered store if it is still offered, otherwise the first one. */
export function pickStore(stores: readonly Store[], preferredId: string | null): Store | null {
  return stores.find((s) => s.id === preferredId) ?? stores[0] ?? null
}
