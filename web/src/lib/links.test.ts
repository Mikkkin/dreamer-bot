import { describe, expect, test } from 'bun:test'
import { isSafeHttpUrl, linkHost, normalizeLinkInput } from './links'

describe('isSafeHttpUrl', () => {
  test.each(['https://example.com', 'http://example.com/a?b=c#d', 'HTTPS://EXAMPLE.COM/x', 'https://xn--80ak6aa92e.com/'])(
    'accepts %p',
    (url) => {
      expect(isSafeHttpUrl(url)).toBe(true)
    },
  )

  test.each([
    'javascript:alert(1)',
    'JaVaScRiPt:alert(1)',
    ' https://example.com',
    'data:text/html,<script>alert(1)</script>',
    'tg://resolve?domain=x',
    'ftp://example.com',
    '//example.com',
    'https://',
    'https://user:pass@example.com/',
    'example.com',
    '',
  ])('rejects %p', (url) => {
    expect(isSafeHttpUrl(url)).toBe(false)
  })
})

describe('normalizeLinkInput', () => {
  test.each([
    ['https://example.com/tour', 'https://example.com/tour'],
    ['  http://example.com  ', 'http://example.com'],
    ['ozon.ru/item/123', 'https://ozon.ru/item/123'],
  ])('%p → %p', (input, url) => {
    expect(normalizeLinkInput(input)).toEqual({ ok: true, url })
  })

  test.each([
    ['', 'Вставьте ссылку'],
    ['javascript://alert(1)', 'Поддерживаются только ссылки http и https'],
    ['ftp://example.com', 'Поддерживаются только ссылки http и https'],
    ['https://user:pw@example.com', 'Ссылки с логином и паролем не поддерживаются'],
    ['javascript:alert(1)', 'Это не похоже на ссылку'],
    [`https://example.com/${'a'.repeat(2048)}`, 'Ссылка слишком длинная'],
  ])('%p is rejected with %p', (input, message) => {
    expect(normalizeLinkInput(input)).toEqual({ ok: false, message })
  })
})

test('linkHost', () => {
  expect(linkHost('https://www.example.com/a')).toBe('example.com')
  expect(linkHost('https://xn--80ak6aa92e.com/')).toBe('xn--80ak6aa92e.com')
  expect(linkHost('not a url')).toBe('')
})
