import { describe, expect, test } from 'bun:test'
import { DEFAULT_RECIPE_EMOJI, recipeEmoji } from './recipeEmoji'

describe('recipeEmoji', () => {
  test('picks a sticker from title keywords, case-insensitively', () => {
    expect(recipeEmoji('Паста карбонара')).toBe('🍝')
    expect(recipeEmoji('Сырники как у бабушки')).toBe('🥞')
    expect(recipeEmoji('БОРЩ')).toBe('🍲')
    expect(recipeEmoji('Тирамису без яиц')).toBe('🍰')
  })

  test('treats ё as е', () => {
    expect(recipeEmoji('Тёплый салат')).toBe('🥗')
  })

  test('short words match only as whole words', () => {
    expect(recipeEmoji('Щи из квашеной капусты')).toBe('🍲')
    expect(recipeEmoji('Фо бо')).toBe('🍲')
    expect(recipeEmoji('Рис с овощами')).toBe('🍚')
    expect(recipeEmoji('Фокачча с розмарином')).toBe('🍞')
    expect(recipeEmoji('Ирис домашний')).toBe(DEFAULT_RECIPE_EMOJI)
  })

  test('the first match wins', () => {
    expect(recipeEmoji('Паста с курицей')).toBe('🍝')
  })

  test('falls back to the default', () => {
    expect(recipeEmoji('Что-то вкусное')).toBe(DEFAULT_RECIPE_EMOJI)
    expect(recipeEmoji('')).toBe(DEFAULT_RECIPE_EMOJI)
  })
})
