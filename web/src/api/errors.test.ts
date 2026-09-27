import { describe, expect, test } from 'bun:test'
import { ApiError, capitalize, networkError, parseErrorEnvelope, parseJsonBody } from './errors'

describe('parseErrorEnvelope', () => {
  test('reads code, message and field', () => {
    const e = parseErrorEnvelope(400, { error: { code: 'validation', message: 'название обязательно', field: 'title' } })
    expect(e).toBeInstanceOf(ApiError)
    expect(e.status).toBe(400)
    expect(e.code).toBe('validation')
    expect(e.message).toBe('Название обязательно')
    expect(e.field).toBe('title')
    expect(e.isAuth).toBe(false)
    expect(e.isRetryable).toBe(false)
  })

  test('auth failures are flagged', () => {
    expect(parseErrorEnvelope(401, { error: { code: 'unauthorized', message: 'x' } }).isAuth).toBe(true)
    expect(parseErrorEnvelope(403, { error: { code: 'forbidden', message: 'x' } }).isAuth).toBe(true)
  })

  test.each([
    [404, null, 'not_found'],
    [413, 'Request Entity Too Large', 'too_large'],
    [415, {}, 'unsupported_media'],
    [422, { error: 'limit' }, 'limit'],
    [429, { error: { code: 42 } }, 'rate_limited'],
    [502, '<html>bad gateway</html>', 'internal'],
    [500, { error: { code: 'something_new', message: 'Упс' } }, 'internal'],
  ])('status %p with body %p falls back to %p', (status, body, code) => {
    const e = parseErrorEnvelope(status, body)
    expect(e.code).toBe(code as ApiError['code'])
    expect(e.message.length).toBeGreaterThan(0)
    expect(e.field).toBeUndefined()
  })

  test('server errors and rate limits are retryable', () => {
    expect(parseErrorEnvelope(503, null).isRetryable).toBe(true)
    expect(parseErrorEnvelope(429, null).isRetryable).toBe(true)
    expect(networkError().isRetryable).toBe(true)
    expect(networkError().status).toBe(0)
  })

  test('ignores a blank message and a non-string field', () => {
    const e = parseErrorEnvelope(400, { error: { code: 'validation', message: '  ', field: 7 } })
    expect(e.message).toBe('Проверьте введённые данные')
    expect(e.field).toBeUndefined()
  })
})

test('parseJsonBody is lenient', () => {
  expect(parseJsonBody('')).toBeNull()
  expect(parseJsonBody('not json')).toBeNull()
  expect(parseJsonBody('{"a":1}')).toEqual({ a: 1 })
})

test('capitalize', () => {
  expect(capitalize('ёлка')).toBe('Ёлка')
  expect(capitalize('')).toBe('')
})
