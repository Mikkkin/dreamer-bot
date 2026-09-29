import { describe, expect, test } from 'bun:test'
import {
  INGREDIENT_FORMS,
  ITEM_FORMS,
  RECIPE_FORMS,
  SERVING_FORMS,
  TIMES_FORMS,
  WISH_FORMS,
  countOf,
  decimalComma,
  formatByline,
  formatDay,
  formatMoney,
  formatShortDay,
  parseMinor,
  percent,
  plural,
  sumByCurrency,
} from './format'

// Intl uses no-break spaces in ru-RU; compare with plain spaces.
const plain = (s: string) => s.replace(/\s/gu, ' ')

describe('parseMinor', () => {
  test.each([
    ['1200', 120000],
    ['1200.5', 120050],
    ['1200.50', 120050],
    ['0.99', 99],
    [' 7 ', 700],
  ])('%p → %p', (input, want) => {
    expect(parseMinor(input)).toBe(want)
  })

  test.each(['', 'abc', '1,5', '1.234', '-5', '1e3'])('rejects %p', (input) => {
    expect(parseMinor(input)).toBeNull()
  })
})

describe('formatMoney', () => {
  test.each([
    [345000, 'EUR', '3 450 €'],
    [120050, 'EUR', '1 200,50 €'],
    [8500000, 'RUB', '85 000 ₽'],
    [1999, 'USD', '19,99 $'],
    [500, 'GBP', '5 £'],
  ])('%p %p → %p', (minor, currency, want) => {
    expect(plain(formatMoney(minor, currency))).toBe(want)
  })

  test('falls back for an unknown currency code', () => {
    expect(plain(formatMoney(1000, 'NOT-A-CODE'))).toBe('10 NOT-A-CODE')
  })
})

describe('sumByCurrency', () => {
  test('sums per currency without conversion, in display order', () => {
    const got = sumByCurrency([
      { amount: '85000', currency: 'RUB' },
      { amount: '1200.50', currency: 'EUR' },
      { amount: '2249.50', currency: 'EUR' },
      { amount: 'garbage', currency: 'EUR' },
      { amount: '10', currency: 'USD' },
    ])
    expect(got).toEqual([
      { currency: 'EUR', minor: 345000, count: 2 },
      { currency: 'USD', minor: 1000, count: 1 },
      { currency: 'RUB', minor: 8500000, count: 1 },
    ])
  })
})

describe('plural', () => {
  test.each([
    [0, 'желаний'],
    [1, 'желание'],
    [2, 'желания'],
    [4, 'желания'],
    [5, 'желаний'],
    [11, 'желаний'],
    [12, 'желаний'],
    [21, 'желание'],
    [22, 'желания'],
    [101, 'желание'],
    [111, 'желаний'],
  ])('%p %p', (n, want) => {
    expect(plural(n, WISH_FORMS)).toBe(want)
  })

  test('countOf joins the number and the form', () => {
    expect(countOf(3, RECIPE_FORMS)).toBe('3 рецепта')
    expect(countOf(25, RECIPE_FORMS)).toBe('25 рецептов')
  })
})

describe('dates', () => {
  const now = new Date(2026, 8, 27, 12, 0, 0)

  test('formatDay omits the current year', () => {
    expect(formatDay(new Date(2026, 8, 12, 15, 0).toISOString(), now)).toBe('12 сентября')
  })

  test('formatDay shows another year', () => {
    expect(plain(formatDay(new Date(2025, 0, 3, 15, 0).toISOString(), now))).toStartWith('3 января 2025')
  })

  test('formatShortDay', () => {
    expect(formatShortDay(new Date(2026, 8, 12, 15, 0).toISOString())).toBe('12 сент.')
  })

  test('invalid dates render as empty strings', () => {
    expect(formatDay('nope', now)).toBe('')
    expect(formatShortDay('nope')).toBe('')
    expect(formatByline('Дима', 'nope', now)).toBe('Дима')
  })

  test('formatByline', () => {
    expect(formatByline('Дима', new Date(2026, 8, 12, 15, 0).toISOString(), now)).toBe('Дима · 12 сентября')
  })
})

test('percent', () => {
  expect(percent(7, 31)).toBe(23)
  expect(percent(0, 0)).toBe(0)
  expect(percent(5, 5)).toBe(100)
})

test('cooking counts and decimals', () => {
  expect(countOf(1, TIMES_FORMS)).toBe('1 раз')
  expect(countOf(3, TIMES_FORMS)).toBe('3 раза')
  expect(countOf(5, TIMES_FORMS)).toBe('5 раз')
  expect(countOf(2, SERVING_FORMS)).toBe('2 порции')
  expect(countOf(21, ITEM_FORMS)).toBe('21 позиция')
  expect(countOf(5, INGREDIENT_FORMS)).toBe('5 ингредиентов')
  expect(decimalComma('4.5')).toBe('4,5')
  expect(decimalComma('4')).toBe('4')
})
