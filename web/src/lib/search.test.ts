import { describe, expect, test } from 'bun:test'
import { foldText, matchesQuery } from './search'

describe('matchesQuery', () => {
  test('is case-insensitive for Cyrillic', () => {
    expect(matchesQuery('КАРБОНАРА', 'Паста карбонара')).toBe(true)
  })

  test('treats ё as е', () => {
    expect(matchesQuery('ежик', 'Пирог «Ёжик»')).toBe(true)
    expect(matchesQuery('Ёж', 'ежевика')).toBe(true)
  })

  test('requires every term, across fields', () => {
    expect(matchesQuery('паста сливок', 'Паста', '200 мл сливок')).toBe(true)
    expect(matchesQuery('паста грибы', 'Паста', '200 мл сливок')).toBe(false)
  })

  test('an empty query matches everything', () => {
    expect(matchesQuery('   ', 'anything')).toBe(true)
  })

  test('normalizes compatibility forms', () => {
    expect(foldText('ＰＡＳＴＡ')).toBe('pasta')
  })
})
