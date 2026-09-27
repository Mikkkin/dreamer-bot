import { describe, expect, test } from 'bun:test'
import { amountForInput, parseAmountInput } from './amount'

describe('parseAmountInput mirrors domain.ParseAmount', () => {
  test.each([
    ['1200', '1200', 120000],
    ['1 200,50', '1200.50', 120050],
    ['1,200.50', '1200.50', 120050],
    ['1.200,5', '1200.50', 120050],
    ['1.200', '1200', 120000],
    ['1,5', '1.50', 150],
    ['0,99', '0.99', 99],
    ["1'000'000", '1000000', 100000000],
    ['12 990', '12990', 1299000],
    ['1_000', '1000', 100000],
  ])('%p → %p', (input, decimal, minor) => {
    expect(parseAmountInput(input)).toEqual({ ok: true, decimal, minor })
  })

  test.each([
    ['', 'Укажите сумму'],
    ['   ', 'Укажите сумму'],
    ['abc', 'Не получилось распознать сумму'],
    ['12.345.678,123', 'Не получилось распознать сумму'],
    ['-5', 'Не получилось распознать сумму'],
    ['0', 'Сумма должна быть больше нуля'],
    ['0,00', 'Сумма должна быть больше нуля'],
    ['1234567890123', 'Слишком большая сумма'],
    ['1000000001', 'Слишком большая сумма'],
  ])('%p is rejected with %p', (input, message) => {
    expect(parseAmountInput(input)).toEqual({ ok: false, message })
  })

  test('the largest allowed amount is accepted', () => {
    expect(parseAmountInput('1000000000')).toEqual({ ok: true, decimal: '1000000000', minor: 100000000000 })
  })
})

test('amountForInput uses a decimal comma', () => {
  expect(amountForInput('1200.50')).toBe('1200,50')
  expect(amountForInput('1200')).toBe('1200')
})
