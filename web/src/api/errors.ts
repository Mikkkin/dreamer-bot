// Parsing of the API error envelope: { "error": { "code", "message", "field"? } }.

export type ErrorCode =
  | 'validation'
  | 'unauthorized'
  | 'forbidden'
  | 'not_found'
  | 'conflict'
  | 'too_large'
  | 'unsupported_media'
  | 'limit'
  | 'not_a_recipe'
  | 'rate_limited'
  | 'internal'
  | 'unavailable'
  | 'network'

const SERVER_CODES: ReadonlySet<string> = new Set<ErrorCode>([
  'validation',
  'unauthorized',
  'forbidden',
  'not_found',
  'conflict',
  'too_large',
  'unsupported_media',
  'limit',
  'not_a_recipe',
  'rate_limited',
  'internal',
  'unavailable',
])

const FALLBACK_MESSAGES: Record<ErrorCode, string> = {
  validation: 'Проверьте введённые данные',
  unauthorized: 'Сессия устарела',
  forbidden: 'Нет доступа',
  not_found: 'Не нашлось',
  conflict: 'Такое уже есть',
  too_large: 'Файл слишком большой',
  unsupported_media: 'Этот формат не поддерживается',
  limit: 'Достигнут лимит',
  not_a_recipe: 'Не нашли в тексте рецепт — вставьте текст с ингредиентами',
  rate_limited: 'Слишком много запросов, подождите немного',
  internal: 'Что-то пошло не так',
  unavailable: 'Сервис сейчас не отвечает, попробуйте позже',
  network: 'Нет соединения',
}

export class ApiError extends Error {
  readonly status: number
  readonly code: ErrorCode
  /** The input field a validation error refers to, e.g. "title". */
  readonly field: string | undefined

  constructor(status: number, code: ErrorCode, message: string, field?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.field = field
  }

  /** The session cannot continue: the initData is invalid or the user is not whitelisted. */
  get isAuth(): boolean {
    return this.status === 401 || this.status === 403
  }

  /** Repeating the same request may succeed (connection trouble or a server hiccup). */
  get isRetryable(): boolean {
    return this.code === 'network' || this.code === 'rate_limited' || this.status >= 500
  }
}

export function codeFromStatus(status: number): ErrorCode {
  switch (status) {
    case 400:
      return 'validation'
    case 401:
      return 'unauthorized'
    case 403:
      return 'forbidden'
    case 404:
      return 'not_found'
    case 409:
      return 'conflict'
    case 413:
      return 'too_large'
    case 415:
      return 'unsupported_media'
    case 422:
      return 'limit'
    case 429:
      return 'rate_limited'
    case 503:
      return 'unavailable'
    default:
      return status === 0 ? 'network' : 'internal'
  }
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

/** Server messages come from the domain in lower case; UI copy starts with a capital. */
export function capitalize(s: string): string {
  return s.length === 0 ? s : s.charAt(0).toLocaleUpperCase('ru') + s.slice(1)
}

/**
 * Builds an ApiError from an HTTP status and the decoded body (any JSON value
 * or null). The status wins over an unknown or missing code, so a proxy error
 * page still maps to a sensible error.
 */
export function parseErrorEnvelope(status: number, body: unknown): ApiError {
  const envelope = isRecord(body) && isRecord(body.error) ? body.error : null
  const rawCode = envelope?.code
  const code = typeof rawCode === 'string' && SERVER_CODES.has(rawCode) ? (rawCode as ErrorCode) : codeFromStatus(status)
  const rawMessage = envelope?.message
  const message =
    typeof rawMessage === 'string' && rawMessage.trim() !== '' ? capitalize(rawMessage.trim()) : FALLBACK_MESSAGES[code]
  const rawField = envelope?.field
  const field = typeof rawField === 'string' && rawField !== '' ? rawField : undefined
  return new ApiError(status, code, message, field)
}

export function networkError(): ApiError {
  return new ApiError(0, 'network', FALLBACK_MESSAGES.network)
}

/** Decodes a JSON body leniently: an empty or non-JSON body yields null. */
export function parseJsonBody(text: string): unknown {
  if (text.trim() === '') return null
  try {
    return JSON.parse(text) as unknown
  } catch {
    return null
  }
}
