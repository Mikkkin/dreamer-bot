import { describe, expect, test } from 'bun:test'
import { checkIngredients, emptyRow, ingredientRows, type IngredientRow } from './ingredients'

const UNITS = ['г', 'кг', 'шт', 'по вкусу']
const limits = { nameMax: 80, max: 50 }
const row = (key: number, name: string, amount = '', unit = ''): IngredientRow => ({ key, name, amount, unit })

describe('checkIngredients', () => {
  test('builds the API list in order', () => {
    const r = checkIngredients([row(1, ' Спагетти ', '320', 'г'), row(2, 'Соль', '', 'по вкусу'), row(3, 'Яйца', '4', 'шт'), row(4, 'Лимон', '1')], UNITS, limits)
    expect(r).toEqual({
      ok: true,
      ingredients: [
        { name: 'Спагетти', amount: '320', unit: 'г' },
        { name: 'Соль', amount: null, unit: 'по вкусу' },
        { name: 'Яйца', amount: '4', unit: 'шт' },
        { name: 'Лимон', amount: '1', unit: null },
      ],
    })
  })

  test('blank rows are skipped', () => {
    expect(checkIngredients([row(1, ''), row(2, 'Мука', '0,5', 'кг'), row(3, '  ', '', 'г')], UNITS, limits)).toEqual({
      ok: true,
      ingredients: [{ name: 'Мука', amount: '0.5', unit: 'кг' }],
    })
  })

  test('each bad row gets its own message', () => {
    const r = checkIngredients(
      [row(1, '', '200', 'г'), row(2, 'Соль', '1', 'по вкусу'), row(3, 'Сахар', 'много'), row(4, 'я'.repeat(81)), row(5, 'Ок', '1')],
      UNITS,
      limits,
    )
    expect(r).toEqual({
      ok: false,
      errors: {
        1: 'Укажите название',
        2: 'Для «по вкусу» количество не указывается',
        3: 'Укажите дробь вида 1/2, 1/3, 1/4 или десятичную, например 0,5',
        4: 'Название не длиннее 80 символов',
      },
    })
  })

  test('fractions and words are sent as canonical decimals', () => {
    expect(
      checkIngredients([row(1, 'Мука', '1 1/2', 'кг'), row(2, 'Сахар', '½', 'шт'), row(3, 'Масло', 'пол', 'кг'), row(4, 'Вода', 'полторы', 'кг')], UNITS, limits),
    ).toEqual({
      ok: true,
      ingredients: [
        { name: 'Мука', amount: '1.5', unit: 'кг' },
        { name: 'Сахар', amount: '0.5', unit: 'шт' },
        { name: 'Масло', amount: '0.5', unit: 'кг' },
        { name: 'Вода', amount: '1.5', unit: 'кг' },
      ],
    })
  })

  test('names are counted in code points, like the server', () => {
    expect(checkIngredients([row(1, '🍅'.repeat(80))], UNITS, limits).ok).toBe(true)
  })

  test('the per-recipe limit', () => {
    const rows = Array.from({ length: 3 }, (_, i) => row(i + 1, `x${i}`))
    expect(checkIngredients(rows, UNITS, { nameMax: 80, max: 2 })).toEqual({ ok: false, errors: {}, message: 'Не больше 2 ингредиентов' })
  })
})

test('ingredientRows and emptyRow', () => {
  const rows = ingredientRows([
    { name: 'Мука', amount: '0.5', unit: 'кг', formatted: '½ кг' },
    { name: 'Соль', amount: null, unit: 'по вкусу', formatted: 'по вкусу' },
    { name: 'Лимон', amount: '1', unit: null, formatted: '1' },
    { name: 'Сливки', amount: '0.33', unit: 'стакан', formatted: '⅓ стакана' },
    { name: 'Масло', amount: '12.5', unit: 'г', formatted: '12,5 г' },
  ])
  // Fraction-friendly units come back as fractions, grams keep the comma.
  expect(rows).toEqual([
    row(1, 'Мука', '½', 'кг'),
    row(2, 'Соль', '', 'по вкусу'),
    row(3, 'Лимон', '1', ''),
    row(4, 'Сливки', '⅓', 'стакан'),
    row(5, 'Масло', '12,5', 'г'),
  ])
  expect(emptyRow(rows).key).toBe(6)
  expect(emptyRow([]).key).toBe(1)
  expect(emptyRow([row(7, 'x'), row(2, 'y')]).key).toBe(8)
})
