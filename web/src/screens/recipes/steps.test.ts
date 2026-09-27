import { describe, expect, test } from 'bun:test'
import { recipeLines } from './steps'

describe('recipeLines', () => {
  test('numbered lines become steps', () => {
    expect(recipeLines('1. Поставить воду.\n2) Обжарить гуанчале')).toEqual([
      { kind: 'step', n: '1', text: 'Поставить воду.' },
      { kind: 'step', n: '2', text: 'Обжарить гуанчале' },
    ])
  })

  test('other lines stay text, blank runs collapse into one gap', () => {
    expect(recipeLines('Ингредиенты:\n200 г гуанчале\n\n\n1. Смешать')).toEqual([
      { kind: 'text', text: 'Ингредиенты:' },
      { kind: 'text', text: '200 г гуанчале' },
      { kind: 'gap' },
      { kind: 'step', n: '1', text: 'Смешать' },
    ])
  })

  test('a number without a separator or text is not a step', () => {
    expect(recipeLines('2024 год\n3.')).toEqual([
      { kind: 'text', text: '2024 год' },
      { kind: 'text', text: '3.' },
    ])
  })

  test('markup stays plain text', () => {
    expect(recipeLines('<b>1. x</b>')).toEqual([{ kind: 'text', text: '<b>1. x</b>' }])
  })

  test('leading and trailing blank lines are dropped; CRLF is normalised', () => {
    expect(recipeLines('\n\r\nСоль\r\n\r\n')).toEqual([{ kind: 'text', text: 'Соль' }])
  })
})
