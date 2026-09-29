import { describe, expect, test } from 'bun:test'
import { amountForEdit, checkQuantity, formatQuantity, parseItemText, parseQuantityAmount } from './quantity'

const UNITS = ['г', 'кг', 'мл', 'л', 'шт', 'ст. л.', 'ч. л.', 'стакан', 'щепотка', 'зубчик', 'пучок', 'упаковка', 'по вкусу']

describe('parseQuantityAmount mirrors domain.parseHundredths', () => {
  test.each([
    ['1,5', '1.5'],
    ['1.5', '1.5'],
    ['200', '200'],
    ['0.25', '0.25'],
    ['0,5', '0.5'],
    ['1.50', '1.5'],
    ['2.', '2'],
    ['1 000', '1000'],
    ['100000', '100000'],
  ])('%p → %p', (input, amount) => {
    expect(parseQuantityAmount(input)).toEqual({ ok: true, amount })
  })

  test.each([
    ['-1', 'Количество — число, например 1,5'],
    ['abc', 'Количество — число, например 1,5'],
    ['1.234', 'Количество — число, например 1,5'],
    ['.5', 'Количество — число, например 1,5'],
    ['9999999', 'Количество — число, например 1,5'],
    ['0', 'Количество должно быть больше нуля'],
    ['0,00', 'Количество должно быть больше нуля'],
    ['100000.01', 'Слишком большое количество'],
  ])('%p is rejected with %p', (input, message) => {
    expect(parseQuantityAmount(input)).toEqual({ ok: false, message })
  })
})

describe('checkQuantity', () => {
  test('amount with a unit', () => {
    expect(checkQuantity('1,5', 'кг', UNITS)).toEqual({ ok: true, value: { amount: '1.5', unit: 'кг' } })
  })

  test('a bare number', () => {
    expect(checkQuantity('3', '', UNITS)).toEqual({ ok: true, value: { amount: '3', unit: null } })
  })

  test('«по вкусу» never has an amount', () => {
    expect(checkQuantity('', 'по вкусу', UNITS)).toEqual({ ok: true, value: { amount: null, unit: 'по вкусу' } })
    expect(checkQuantity('1', 'по вкусу', UNITS)).toEqual({ ok: false, message: 'Для «по вкусу» количество не указывается' })
  })

  test('nothing is "not specified"; a unit without an amount is an error, not dropped', () => {
    expect(checkQuantity('', '', UNITS)).toEqual({ ok: true, value: { amount: null, unit: null } })
    expect(checkQuantity(' ', 'г', UNITS)).toEqual({ ok: false, message: 'Укажите количество' })
  })

  test('only known units are sent', () => {
    expect(checkQuantity('2', 'ведро', UNITS)).toEqual({ ok: false, message: 'Неизвестная единица измерения' })
  })
})

test('formatQuantity matches Quantity.Format', () => {
  expect(formatQuantity('1.5', 'кг')).toBe('1,5\u00a0кг')
  expect(formatQuantity('3', null)).toBe('3')
  expect(formatQuantity(null, 'по вкусу')).toBe('по вкусу')
  expect(formatQuantity(null, null)).toBe('')
  expect(amountForEdit('0.25')).toBe('0,25')
  expect(amountForEdit(null)).toBe('')
})

describe('parseItemText', () => {
  test.each([
    ['Молоко 1 л', { name: 'Молоко', amount: '1', unit: 'л' }],
    ['Молоко 1,5л', { name: 'Молоко', amount: '1.5', unit: 'л' }],
    ['Мука 500г', { name: 'Мука', amount: '500', unit: 'г' }],
    ['Мука 500 гр', { name: 'Мука', amount: '500', unit: 'г' }],
    ['Яйца 10 шт.', { name: 'Яйца', amount: '10', unit: 'шт' }],
    ['Сахар 2 ст. л.', { name: 'Сахар', amount: '2', unit: 'ст. л.' }],
    ['Сахар 2 ст.л.', { name: 'Сахар', amount: '2', unit: 'ст. л.' }],
    ['Мёд 1 стакан', { name: 'Мёд', amount: '1', unit: 'стакан' }],
    ['Соль по вкусу', { name: 'Соль', amount: null, unit: 'по вкусу' }],
    ['Лимоны 3', { name: 'Лимоны', amount: '3', unit: null }],
    ['  Сыр   моцарелла  250 г ', { name: 'Сыр моцарелла', amount: '250', unit: 'г' }],
    ['Кока-кола 2 Л', { name: 'Кока-кола', amount: '2', unit: 'л' }],
  ])('%p', (text, want) => {
    expect(parseItemText(text, UNITS)).toEqual(want)
  })

  test.each(['Хлеб', 'Пицца 4 сыра', '5 яиц', '7up', 'Вода 0', 'Вода 1.234 л'])('%p stays a plain name', (text) => {
    const r = parseItemText(text, UNITS)
    expect(r.amount).toBeNull()
    expect(r.unit === null || r.unit === 'по вкусу').toBe(true)
  })

  test('units missing from Me.units are not recognised', () => {
    expect(parseItemText('Молоко 1 л', ['г'])).toEqual({ name: 'Молоко 1 л', amount: null, unit: null })
  })
})
