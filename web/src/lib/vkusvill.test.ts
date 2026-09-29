import { describe, expect, test } from 'bun:test'
import type { ShoppingItem, VkusvillMatch } from '../api/types'
import {
  cartLines,
  estimateCart,
  initialChoices,
  isVkusvillBasketUrl,
  parseCartQuantity,
  stepCartQuantity,
  type CartChoice,
} from './vkusvill'

const rub = (amount: string) => ({ amount, currency: 'RUB', formatted: `${amount} ₽` })

const MATCHES: VkusvillMatch[] = [
  {
    item_id: 31,
    query: 'Молоко',
    candidates: [
      { xml_id: 173, name: 'Молоко 3,2%, 1 л', price: rub('93'), unit: 'шт', weight: '1 л' },
      { xml_id: 174, name: 'Молоко 2,5%, 1 л', price: rub('85.50'), unit: 'шт', weight: '1 л' },
    ],
  },
  { item_id: 32, query: 'Яйца', candidates: [{ xml_id: 200, name: 'Яйца С0, 10 шт', price: rub('139'), unit: 'шт', weight: '10 шт' }] },
  { item_id: 33, query: 'Рамбутан', candidates: [] },
  { item_id: 34, query: 'Сыр', candidates: [{ xml_id: 300, name: 'Сыр', price: null, unit: 'кг', weight: null }] },
]

function item(id: number, amount: string | null, unit: string | null): ShoppingItem {
  return {
    id,
    name: `i${id}`,
    quantity: amount || unit ? { amount, unit, formatted: '' } : null,
    checked: false,
    recipe_id: null,
    added_by: { id: 1, name: 'Дима' },
    created_at: '2026-09-01T10:00:00Z',
    updated_at: '2026-09-01T10:00:00Z',
  }
}

describe('parseCartQuantity: 0.01–40', () => {
  test.each([
    ['2', '2', 200],
    ['1,5', '1.5', 150],
    ['0.25', '0.25', 25],
    ['0,01', '0.01', 1],
    ['40', '40', 4000],
  ])('%p → %p', (raw, quantity, hundredths) => {
    expect(parseCartQuantity(raw)).toEqual({ ok: true, quantity, hundredths })
  })

  test.each(['', '0', '0,00', '40.01', '41', '-1', '1.234', 'два'])('rejects %p', (raw) => {
    expect(parseCartQuantity(raw).ok).toBe(false)
  })
})

test('stepCartQuantity', () => {
  expect(stepCartQuantity('1', 1, 'шт')).toBe('2')
  expect(stepCartQuantity('1', -1, 'шт')).toBe('1')
  expect(stepCartQuantity('40', 1, 'шт')).toBe('40')
  expect(stepCartQuantity('0,5', 1, 'кг')).toBe('0,6')
  expect(stepCartQuantity('0,1', -1, 'кг')).toBe('0,1')
  expect(stepCartQuantity('abc', 1, 'шт')).toBe('2')
  expect(stepCartQuantity('2', 1, null)).toBe('3')
})

test('initialChoices preselects the best product and carries pieces over', () => {
  const choices = initialChoices(MATCHES, [item(31, '2', 'л'), item(32, '3', 'шт'), item(34, '1.5', 'шт')])
  expect(choices.get(31)).toEqual({ xmlId: 173, quantity: '1' })
  expect(choices.get(32)).toEqual({ xmlId: 200, quantity: '3' })
  expect(choices.get(33)).toEqual({ xmlId: null, quantity: '1' })
  expect(choices.get(34)).toEqual({ xmlId: 300, quantity: '1' })
})

describe('cartLines', () => {
  const choices = (entries: [number, CartChoice][]) => new Map(entries)

  test('only chosen products, canonical quantities', () => {
    const r = cartLines(
      MATCHES,
      choices([
        [31, { xmlId: 174, quantity: '2' }],
        [32, { xmlId: null, quantity: '1' }],
        [34, { xmlId: 300, quantity: '0,5' }],
      ]),
    )
    expect(r).toEqual({
      ok: true,
      lines: [
        { xml_id: 174, quantity: '2' },
        { xml_id: 300, quantity: '0.5' },
      ],
    })
  })

  test('a product that was not offered for that item is refused', () => {
    const r = cartLines(MATCHES, choices([[31, { xmlId: 200, quantity: '1' }]]))
    expect(r.ok).toBe(false)
    if (!r.ok) expect(r.errors.get(31)).toBe('Выберите товар из списка')
  })

  test('bad quantities are reported per item', () => {
    const r = cartLines(MATCHES, choices([[31, { xmlId: 173, quantity: '50' }]]))
    expect(r.ok).toBe(false)
    if (!r.ok) expect(r.errors.get(31)).toBe('Не больше 40')
  })

  test('nothing chosen', () => {
    expect(cartLines(MATCHES, new Map())).toEqual({ ok: false, errors: new Map(), message: 'Выберите хотя бы один товар' })
  })

  test('at most 30 lines', () => {
    const many: VkusvillMatch[] = Array.from({ length: 31 }, (_, i) => ({
      item_id: i + 1,
      query: 'x',
      candidates: [{ xml_id: i + 1, name: 'x', price: null, unit: null, weight: null }],
    }))
    const r = cartLines(many, initialChoices(many, []))
    expect(r).toMatchObject({ ok: false, message: 'Не больше 30 товаров за раз' })
  })
})

test('estimateCart sums price × quantity and flags missing prices', () => {
  const est = estimateCart(
    MATCHES,
    new Map([
      [31, { xmlId: 174, quantity: '2' }],
      [32, { xmlId: 200, quantity: '1' }],
    ]),
  )
  expect(est).toEqual({ minor: 17100 + 13900, currency: 'RUB', complete: true, count: 2 })
  const partial = estimateCart(MATCHES, new Map([[34, { xmlId: 300, quantity: '1' }]]))
  expect(partial).toEqual({ minor: 0, currency: 'RUB', complete: false, count: 1 })
  expect(estimateCart(MATCHES, new Map())).toBeNull()
})

describe('isVkusvillBasketUrl', () => {
  test.each(['https://vkusvill.ru/?share_basket=2063312749', 'https://www.vkusvill.ru/cart/'])('accepts %p', (url) => {
    expect(isVkusvillBasketUrl(url)).toBe(true)
  })

  test.each([
    'http://vkusvill.ru/?share_basket=1',
    'https://vkusvill.ru.evil.example/',
    'https://evilvkusvill.ru/',
    'https://vkusvill.ru:8443/',
    'https://user:pw@vkusvill.ru/',
    'javascript:alert(1)',
    'https://example.com/?u=https://vkusvill.ru/',
  ])('refuses %p', (url) => {
    expect(isVkusvillBasketUrl(url)).toBe(false)
  })
})
