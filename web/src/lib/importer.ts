// Importing a recipe from an Instagram post: what the import sheet sends
// and how it reacts to each answer. The server owns the parsing; the client
// only finds the link, checks the limits and picks the next step.

import type { ApiError } from '../api/errors'
import type { ImportInput } from '../api/types'

/** The server's limits for POST /api/recipes/import. */
export const IMPORT_URL_MAX = 2048
export const IMPORT_TEXT_MAX = 10_000

const HOSTS: ReadonlySet<string> = new Set(['instagram.com', 'www.instagram.com', 'm.instagram.com'])
const KINDS: Readonly<Record<string, string>> = { p: 'p', reel: 'reel', reels: 'reel', tv: 'tv' }
const SHORTCODE = /^[A-Za-z0-9_-]{5,40}$/

/**
 * Finds an Instagram post or reel link in what was pasted («Смотри
 * https://www.instagram.com/reel/Cx…/?igsh=…») and rebuilds it the way the
 * server does: https://www.instagram.com/{p|reel|tv}/{code}/, without the
 * query or the fragment. null when there is no such link.
 */
export function instagramUrl(raw: string): string | null {
  for (const token of raw.split(/\s+/u)) {
    const candidate = token.replace(/^[<(«"']+|[>)»"'.,;!]+$/gu, '')
    if (!/instagram\.com\//iu.test(candidate)) continue
    let url: URL
    try {
      url = new URL(/^https?:\/\//iu.test(candidate) ? candidate : `https://${candidate}`)
    } catch {
      continue
    }
    if ((url.protocol !== 'https:' && url.protocol !== 'http:') || !HOSTS.has(url.hostname.toLowerCase())) continue
    const found = postPath(url.pathname.split('/').filter((s) => s !== ''))
    if (found) return found
  }
  return null
}

// Instagram's own first path segments: never a username in /{user}/reel/{code}/.
const RESERVED = new Set(['share', 'explore', 'stories', 'accounts', 'direct', 'about', 'developer', 'legal'])

/** /p/CODE/… or /username/p/CODE/… as on the server (recipeimport.ParseURL). */
function postPath(segments: readonly string[]): string | null {
  for (let i = 0; i <= 1; i++) {
    const kind = segments[i]
    const code = segments[i + 1]
    if (kind === undefined || code === undefined) break
    const canonical = KINDS[kind.toLowerCase()]
    if (!canonical) continue
    if (i === 1 && RESERVED.has((segments[0] ?? '').toLowerCase())) return null
    return SHORTCODE.test(code) ? `https://www.instagram.com/${canonical}/${code}/` : null
  }
  return null
}

export type ImportMode = 'link' | 'text'

export type ImportCheck = { ok: true; input: ImportInput } | { ok: false; message: string }

const runes = (s: string) => [...s].length

/** Validates the sheet's field before the request, with the same limits as the server. */
export function checkImport(mode: ImportMode, raw: string): ImportCheck {
  const value = raw.trim()
  if (mode === 'link') {
    if (value === '') return { ok: false, message: 'Вставьте ссылку на пост' }
    const url = instagramUrl(value)
    if (!url) return { ok: false, message: 'Это не ссылка на пост или рилс в Instagram. Можно вставить текст подписи — «Вставить текст».' }
    if (url.length > IMPORT_URL_MAX) return { ok: false, message: 'Слишком длинная ссылка' }
    return { ok: true, input: { url } }
  }
  if (value === '') return { ok: false, message: 'Вставьте текст рецепта' }
  if (runes(value) > IMPORT_TEXT_MAX) return { ok: false, message: `Не длиннее ${IMPORT_TEXT_MAX} символов` }
  return { ok: true, input: { text: value } }
}

/**
 * What the progress line says: a link is read first, text goes straight to
 * the ingredients. A link import still running after two stages is most
 * likely watching the reel's video, which takes up to a minute or so.
 */
export function importStages(mode: ImportMode): readonly string[] {
  return mode === 'link'
    ? ['Читаем пост…', 'Раскладываем ингредиенты…', 'Смотрим видео — это может занять до минуты…']
    : ['Раскладываем ингредиенты…']
}

/** How long each stage of importStages stays before the next one: the video stage shows after 8 s. */
export const IMPORT_STAGE_MS = 4000

export type ImportFailure =
  /** Instagram did not give the post: offer the text field with the server's message. */
  | { kind: 'paste-text'; message: string }
  /** Show the message by the field. */
  | { kind: 'message'; message: string }

/** The next step after a failed import. */
export function importFailure(error: ApiError, mode: ImportMode): ImportFailure {
  if (mode === 'link' && error.code === 'unavailable') return { kind: 'paste-text', message: error.message }
  return { kind: 'message', message: error.message }
}
