import { describe, expect, test } from 'bun:test'
import type { Price, Wish, WishSaved } from '../api/types'
import { formatSavedOfPrice, openSavings, savedPercent, savingsCurrency, savingsProgress } from './savings'

const plain = (s: string) => s.replace(/\s/gu, ' ')
const price = (amount: string, currency = 'RUB'): Price => ({ amount, currency, formatted: '' })
const saved = (amount: string, currency = 'RUB', count = 1): WishSaved => ({ total: price(amount, currency), percent: null, count })

describe('savedPercent mirrors domain.Wish.SavedPercent', () => {
  test.each([
    [1_200_000, 4_500_000, 26],
    [4_499_999, 4_500_000, 99],
    [4_500_000, 4_500_000, 100],
    [9_000_000, 4_500_000, 100],
    [0, 4_500_000, 0],
    [100, 0, 0],
  ])('%p of %p → %p', (s, p, want) => {
    expect(savedPercent(s, p)).toBe(want)
  })
})

describe('savingsProgress', () => {
  test('with a price', () => {
    expect(savingsProgress({ price: price('45000'), saved: saved('12000') })).toEqual({
      savedMinor: 1_200_000,
      priceMinor: 4_500_000,
      currency: 'RUB',
      percent: 26,
      complete: false,
    })
  })

  test('complete only when everything is saved', () => {
    expect(savingsProgress({ price: price('100'), saved: saved('99.99') })).toMatchObject({ percent: 99, complete: false })
    expect(savingsProgress({ price: price('100'), saved: saved('100') })).toMatchObject({ percent: 100, complete: true })
    expect(savingsProgress({ price: price('100'), saved: saved('150') })).toMatchObject({ percent: 100, complete: true })
  })

  test('without a price, or in another currency, there is no percent', () => {
    expect(savingsProgress({ price: null, saved: saved('5000') })).toEqual({
      savedMinor: 500_000,
      priceMinor: null,
      currency: 'RUB',
      percent: null,
      complete: false,
    })
    expect(savingsProgress({ price: price('10', 'EUR'), saved: saved('5000') })).toMatchObject({ percent: null })
  })

  test('nothing saved', () => {
    expect(savingsProgress({ price: price('100'), saved: null })).toBeNull()
  })
})

test('savingsCurrency: the price first, then what is saved, else free choice', () => {
  expect(savingsCurrency({ price: price('10', 'EUR'), saved: null }, 'RUB')).toEqual({ currency: 'EUR', locked: true })
  expect(savingsCurrency({ price: null, saved: saved('5', 'USD') }, 'RUB')).toEqual({ currency: 'USD', locked: true })
  expect(savingsCurrency({ price: null, saved: null }, 'RUB')).toEqual({ currency: 'RUB', locked: false })
})

test('formatSavedOfPrice shows the currency once', () => {
  expect(plain(formatSavedOfPrice(1_200_000, 4_500_000, 'RUB'))).toBe('12 000 / 45 000 ₽')
  expect(plain(formatSavedOfPrice(12_050, 345_000, 'EUR'))).toBe('120,50 / 3 450 €')
})

test('openSavings ignores fulfilled wishes', () => {
  const wishes: Pick<Wish, 'status' | 'saved'>[] = [
    { status: 'progress', saved: saved('12000') },
    { status: 'want', saved: saved('5000') },
    { status: 'done', saved: saved('999') },
    { status: 'progress', saved: saved('10', 'EUR') },
    { status: 'progress', saved: null },
  ]
  expect(openSavings(wishes)).toEqual([
    { currency: 'EUR', minor: 1000, count: 1 },
    { currency: 'RUB', minor: 1_700_000, count: 2 },
  ])
})
