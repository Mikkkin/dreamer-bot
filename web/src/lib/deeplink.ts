// Deep links: the bot opens the app with WEBAPP_URL?wish={id} or ?recipe={id}.
// A t.me "startapp" parameter of the form w_{id} / r_{id} is accepted too. The
// hash is reserved for Telegram's launch parameters and is never read here.

export type DeepLink = { kind: 'wish' | 'recipe'; id: number }

const ID_RE = /^[1-9]\d{0,14}$/
const START_PARAM_RE = /^([wr])_([1-9]\d{0,14})$/

function parseId(raw: string | null | undefined): number | null {
  if (!raw || !ID_RE.test(raw)) return null
  const n = Number(raw)
  return Number.isSafeInteger(n) ? n : null
}

export function parseDeepLink(search: string, startParam?: string | null): DeepLink | null {
  const q = new URLSearchParams(search)
  const wish = parseId(q.get('wish'))
  if (wish !== null) return { kind: 'wish', id: wish }
  const recipe = parseId(q.get('recipe'))
  if (recipe !== null) return { kind: 'recipe', id: recipe }

  const m = startParam ? START_PARAM_RE.exec(startParam) : null
  const id = parseId(m?.[2])
  if (m && id !== null) return { kind: m[1] === 'w' ? 'wish' : 'recipe', id }
  return null
}
