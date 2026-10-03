import { describe, expect, test } from 'bun:test'
import type { Ingredient } from '../api/types'
import {
  checkServings,
  exactAmount,
  formatFactor,
  scaleFactor,
  scaleIngredient,
  scaleQuantity,
  scaledShoppingItems,
  snapCount,
  stepServings,
} from './servings'

const NB = ' '
const ing = (name: string, amount: string | null, unit: string | null): Ingredient => ({ name, amount, unit, formatted: '' })

/** The scaled text of one line. */
const scaled = (amount: string | null, unit: string | null, factor: number) => scaleIngredient(ing('x', amount, unit), factor).text

describe('factor', () => {
  test('chosen / base', () => {
    expect(scaleFactor(4, 6)).toBe(1.5)
    expect(scaleFactor(2, 2)).toBe(1)
    expect(scaleFactor(0, 3)).toBe(1)
  })

  test('the badge', () => {
    expect(formatFactor(1.5)).toBe('×1,5')
    expect(formatFactor(2)).toBe('×2')
    expect(formatFactor(4 / 3)).toBe('×1,33')
    expect(formatFactor(1 / 4)).toBe('×0,25')
  })

  test('the form field', () => {
    expect(checkServings('', 50)).toEqual({ ok: true, value: null })
    expect(checkServings(' 4 ', 50)).toEqual({ ok: true, value: 4 })
    expect(checkServings('50', 50)).toEqual({ ok: true, value: 50 })
    for (const bad of ['0', '51', '2,5', 'abc', '-1']) {
      expect(checkServings(bad, 50)).toEqual({ ok: false, message: 'Порций — целое число от 1 до 50' })
    }
  })

  test('the stepper stays within 1..max', () => {
    expect(stepServings(1, -1, 50)).toBe(1)
    expect(stepServings(50, 1, 50)).toBe(50)
    expect(stepServings(4, 1, 50)).toBe(5)
  })
})

describe('stored thirds are exact before scaling', () => {
  test('⅓ × 3 is 1, never 0,99', () => {
    expect(exactAmount(33)).toBeCloseTo(1 / 3, 10)
    expect(exactAmount(167)).toBeCloseTo(5 / 3, 10)
    expect(scaled('0.33', 'стакан', 3)).toBe(`1${NB}стакан`)
    expect(scaled('0.67', 'ч. л.', 1.5)).toBe(`1${NB}чайная ложка`)
    expect(scaled('0.33', null, 3)).toBe('1')
  })
})

describe('counts snap to ¼ ⅓ ½ ⅔ ¾ within 0.05, else one decimal', () => {
  test.each([
    [1, 100],
    [1.04, 100],
    [0.97, 100],
    [1.5, 150],
    [1.46, 150],
    [0.26, 25],
    [0.31, 33],
    [0.7, 67],
    [0.74, 75],
    [1.6, 160],
    [2.4, 240],
    [0.12, 10],
    [0.04, 4],
  ])('%p → %p hundredths', (v, h) => {
    expect(snapCount(v)).toBe(h)
  })

  test.each([
    // Pieces: 1 шт × 1.5 = 1½ шт; under ¼, one decimal.
    ['1', 'шт', 1.5, `1½${NB}шт`],
    ['1', 'шт', 0.125, `0,1${NB}шт`],
    ['2', 'ст. л.', 1.5, `3${NB}столовые ложки`],
    ['1', 'ч. л.', 0.5, `½${NB}чайной ложки`],
    ['3', 'стакан', 2, `6${NB}стаканов`],
    ['1.5', 'кг', 2, `3${NB}кг`],
    ['0.75', 'л', 1 / 3, `¼${NB}л`],
    ['2', null, 1.5, '3'],
    ['1', 'зубчик', 5 / 4, `1¼${NB}зубчика`],
    ['3', 'шт', 4 / 3, `4${NB}шт`],
    ['1', 'щепотка', 3, `3${NB}щепотки`],
  ] as const)('%p %p × %p → %p', (amount, unit, factor, want) => {
    expect(scaled(amount, unit, factor)).toBe(want)
  })
})

describe('grams and millilitres round like a kitchen scale', () => {
  test.each([
    ['3', 'г', 1.5, `5${NB}г`], // 4.5 → whole
    ['5', 'г', 1.5, `8${NB}г`], // 7.5 → whole, half up
    ['15', 'г', 1.5, `25${NB}г`], // 22.5 → steps of 5
    ['30', 'мл', 1.5, `45${NB}мл`],
    ['70', 'г', 1.5, `110${NB}г`], // 105 → steps of 10, half up
    ['320', 'г', 1.5, `480${NB}г`],
    ['333', 'г', 0.5, `170${NB}г`], // 166.5 → 170
    ['800', 'г', 1.5, `1,2${NB}кг`],
    ['750', 'мл', 2, `1,5${NB}л`],
    ['1000', 'г', 1.25, `1,3${NB}кг`], // 1250 → one decimal, half up
    ['0.5', 'г', 0.5, `0,3${NB}г`], // never 0
  ] as const)('%p %p × %p', (amount, unit, factor, want) => {
    expect(scaled(amount, unit, factor)).toBe(want)
  })
})

describe('what never scales', () => {
  test('«по вкусу» and lines without an amount', () => {
    expect(scaleIngredient(ing('Соль', null, 'по вкусу'), 2)).toEqual({ name: 'Соль', text: 'по вкусу', amount: null, unit: 'по вкусу', scaled: false })
    expect(scaleIngredient(ing('Зелень', null, null), 2)).toEqual({ name: 'Зелень', text: '', amount: null, unit: null, scaled: false })
    expect(scaleQuantity(null, 'г', 2)).toBeNull()
  })

  test('the factor 1 keeps the stored amount exactly', () => {
    expect(scaleIngredient(ing('Сливки', '0.33', 'стакан'), 1)).toEqual({
      name: 'Сливки',
      text: `⅓${NB}стакана`,
      amount: '0.33',
      unit: 'стакан',
      scaled: false,
    })
    expect(scaleIngredient(ing('Мука', '12.5', 'г'), 1).text).toBe(`12,5${NB}г`)
  })

  test('labels re-decline for the new amount', () => {
    expect(scaled('1', 'ч. л.', 2)).toBe(`2${NB}чайные ложки`)
    expect(scaled('1', 'ч. л.', 5)).toBe(`5${NB}чайных ложек`)
    expect(scaled('1', 'ч. л.', 21)).toBe(`21${NB}чайная ложка`)
  })

  test('a changed amount is marked as scaled; an equal one is not', () => {
    expect(scaleIngredient(ing('Яйца', '2', 'шт'), 1.5).scaled).toBe(true)
    // 10 г × 1.04 = 10,4 г rounds back to 10 г.
    expect(scaleIngredient(ing('Соль', '10', 'г'), 1.04).scaled).toBe(false)
  })
})

test('the shopping list gets the scaled amounts of the picked lines, in recipe order', () => {
  const list = [ing('Спагетти', '320', 'г'), ing('Соль', null, 'по вкусу'), ing('Яйца', '4', 'шт'), ing('Молоко', '750', 'мл')]
  expect(scaledShoppingItems(list, new Set([3, 0, 1]), 2)).toEqual([
    { name: 'Спагетти', amount: '640', unit: 'г' },
    { name: 'Соль', amount: null, unit: 'по вкусу' },
    { name: 'Молоко', amount: '1.5', unit: 'л' },
  ])
  expect(scaledShoppingItems(list, [2], 0.75)).toEqual([{ name: 'Яйца', amount: '3', unit: 'шт' }])
})
