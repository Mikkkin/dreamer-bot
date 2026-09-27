import { describe, expect, test } from 'bun:test'
import { glueShortWords } from './typo'

const NB = ' '

describe('glueShortWords', () => {
  test('glues short words to the next one', () => {
    expect(glueShortWords('Поездка в Токио на сакуру')).toBe(`Поездка в${NB}Токио на${NB}сакуру`)
  })

  test('handles runs of short words and quotes', () => {
    expect(glueShortWords('Я и ты в «Ла Скала»')).toBe(`Я${NB}и${NB}ты${NB}в${NB}«Ла Скала»`)
    expect(glueShortWords('Фильм «В бой идут»')).toBe(`Фильм «В${NB}бой идут»`)
  })

  test('does not glue short nouns such as «ям»', () => {
    expect(glueShortWords('Том ям с креветками')).toBe(`Том ям с${NB}креветками`)
    expect(glueShortWords('Суп фо на обед')).toBe(`Суп фо на${NB}обед`)
  })

  test('leaves longer words, Latin and edges alone', () => {
    expect(glueShortWords('Ужин со звездой Мишлен')).toBe(`Ужин со${NB}звездой Мишлен`)
    expect(glueShortWords('Trip to NY')).toBe('Trip to NY')
    expect(glueShortWords('в')).toBe('в')
    expect(glueShortWords('')).toBe('')
  })
})
