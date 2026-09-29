import { describe, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import type { Store } from '../api/types'
import { STORE_HOSTS, isValidStoreTemplate, pickStore, storeSearchUrl, usableStores } from './stores'

const VV = 'https://vkusvill.ru/search/?q={q}'

describe('storeSearchUrl', () => {
  test('encodes the name as one URI component', () => {
    expect(storeSearchUrl(VV, 'Молоко')).toBe('https://vkusvill.ru/search/?q=%D0%9C%D0%BE%D0%BB%D0%BE%D0%BA%D0%BE')
    expect(storeSearchUrl('https://magnit.ru/s?q={q}', '  сыр   & вино ')).toBe('https://magnit.ru/s?q=%D1%81%D1%8B%D1%80%20%26%20%D0%B2%D0%B8%D0%BD%D0%BE')
  })

  test('a query can not add parameters, fragments or paths', () => {
    const url = storeSearchUrl('https://magnit.ru/s?q={q}', 'a&admin=1#x/../y?z')
    expect(url).toBe('https://magnit.ru/s?q=a%26admin%3D1%23x%2F..%2Fy%3Fz')
    expect(new URL(url ?? '').searchParams.get('admin')).toBeNull()
  })

  test('a {q} inside the query is not expanded again', () => {
    expect(storeSearchUrl('https://magnit.ru/s?q={q}', '{q}')).toBe('https://magnit.ru/s?q=%7Bq%7D')
  })

  test('a slot in the path is fine', () => {
    expect(storeSearchUrl('https://magnit.ru/search/{q}/', 'хлеб')).toBe('https://magnit.ru/search/%D1%85%D0%BB%D0%B5%D0%B1/')
  })

  test.each([
    ['http://shop.example/s?q={q}', 'plain http'],
    ['javascript:alert(1)//{q}', 'javascript'],
    ['JAVASCRIPT://{q}', 'javascript with slashes'],
    ['data:text/html,{q}', 'data'],
    ['https://shop.example/s', 'no slot'],
    ['https://{q}.evil.example/', 'slot in the host'],
    ['https://user:pw@shop.example/?q={q}', 'credentials'],
    ['//shop.example/?q={q}', 'protocol-relative'],
    [' https://shop.example/?q={q}', 'leading space'],
    ['https://shop.example/s?q={q}', 'unknown host'],
    ['https://vkusvill.ru.evil.example/?q={q}', 'known host as a prefix'],
    ['', 'empty'],
  ])('refuses %p (%s)', (template) => {
    expect(storeSearchUrl(template, 'молоко')).toBeNull()
  })

  test('an empty name gives no link', () => {
    expect(storeSearchUrl(VV, '   ')).toBeNull()
  })
})

test('isValidStoreTemplate', () => {
  expect(isValidStoreTemplate(VV)).toBe(true)
  expect(isValidStoreTemplate('HTTPS://magnit.ru/?q={q}')).toBe(true)
  expect(isValidStoreTemplate('https://shop.example/?q={q}')).toBe(false)
  expect(isValidStoreTemplate('https://magnit.ru/?q=')).toBe(false)
})

describe('store choice', () => {
  const stores: Store[] = [
    { id: 'vkusvill', name: 'ВкусВилл', emoji: '🥬', search_url_template: VV, opens_app: false, cart: true },
    { id: 'bad', name: 'Bad', emoji: '💀', search_url_template: 'http://x.example/?q={q}', opens_app: false, cart: false },
    { id: 'perekrestok', name: 'Перекрёсток', emoji: '🛒', search_url_template: 'https://www.perekrestok.ru/cat/search?search={q}', opens_app: true, cart: false },
  ]

  test('usableStores drops unsafe templates', () => {
    expect(usableStores(stores).map((s) => s.id)).toEqual(['vkusvill', 'perekrestok'])
  })

  test('pickStore remembers a still-offered store', () => {
    const usable = usableStores(stores)
    expect(pickStore(usable, 'perekrestok')?.id).toBe('perekrestok')
    expect(pickStore(usable, 'bad')?.id).toBe('vkusvill')
    expect(pickStore(usable, null)?.id).toBe('vkusvill')
    expect(pickStore([], 'vkusvill')).toBeNull()
  })
})

test('STORE_HOSTS lists exactly the hosts of the server catalog', () => {
  const catalog = readFileSync(new URL('../../../internal/stores/stores.go', import.meta.url), 'utf8')
  const hosts = [...catalog.matchAll(/SearchTemplate: "https:\/\/([^/"]+)/g)].map((m) => m[1])
  expect(hosts.length).toBeGreaterThanOrEqual(10)
  expect(new Set(hosts)).toEqual(new Set(STORE_HOSTS))
})
