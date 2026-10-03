import { describe, expect, test } from 'bun:test'
import { ApiError } from '../api/errors'
import { IMPORT_STAGE_MS, IMPORT_TEXT_MAX, checkImport, importFailure, importStages, instagramUrl } from './importer'

describe('instagramUrl finds the post and rebuilds it like the server', () => {
  test.each([
    ['https://www.instagram.com/reel/DIqGZbzMxyz/', 'https://www.instagram.com/reel/DIqGZbzMxyz/'],
    ['https://instagram.com/p/CxYz_12-ab/?igsh=MTc4&utm_source=ig', 'https://www.instagram.com/p/CxYz_12-ab/'],
    ['https://m.instagram.com/reels/Abcde12345#comments', 'https://www.instagram.com/reel/Abcde12345/'],
    ['http://www.instagram.com/tv/Abcde12345', 'https://www.instagram.com/tv/Abcde12345/'],
    ['instagram.com/reel/Abcde12345', 'https://www.instagram.com/reel/Abcde12345/'],
    ['Смотри, какая паста! https://www.instagram.com/reel/Abcde12345/?igsh=x — сохрани', 'https://www.instagram.com/reel/Abcde12345/'],
    ['«https://www.instagram.com/p/Abcde12345/».', 'https://www.instagram.com/p/Abcde12345/'],
    ['https://www.instagram.com/chef_anna/reel/Abcde12345/', 'https://www.instagram.com/reel/Abcde12345/'],
    ['instagram.com/chef_anna/p/Abcde12345?igsh=1', 'https://www.instagram.com/p/Abcde12345/'],
  ])('%p', (raw, want) => {
    expect(instagramUrl(raw)).toBe(want)
  })

  test.each([
    'https://www.instagram.com/demo_kitchen/',
    'https://www.instagram.com/reel/abc/',
    'https://evil.example/instagram.com/reel/Abcde12345/',
    'https://instagram.com.evil.example/reel/Abcde12345/',
    'https://www.instagram.com/stories/user/123456789/',
    'https://www.instagram.com/share/reel/BAabcdefgh12/',
    'https://www.instagram.com/explore/p/Abcde12345/',
    'javascript:alert(1)//instagram.com/reel/Abcde12345',
    'просто текст',
  ])('%p is not a post', (raw) => {
    expect(instagramUrl(raw)).toBeNull()
  })
})

describe('checkImport', () => {
  test('a link is sent in its canonical form', () => {
    expect(checkImport('link', ' https://instagram.com/reel/Abcde12345/?igsh=1 ')).toEqual({
      ok: true,
      input: { url: 'https://www.instagram.com/reel/Abcde12345/' },
    })
  })

  test('a link field without a post', () => {
    expect(checkImport('link', '')).toEqual({ ok: false, message: 'Вставьте ссылку на пост' })
    expect(checkImport('link', 'https://example.com/recipe').ok).toBe(false)
  })

  test('text is trimmed and capped like the server (code points)', () => {
    expect(checkImport('text', '  Паста\nСпагетти 200 г  ')).toEqual({ ok: true, input: { text: 'Паста\nСпагетти 200 г' } })
    expect(checkImport('text', ' ')).toEqual({ ok: false, message: 'Вставьте текст рецепта' })
    expect(checkImport('text', '🍝'.repeat(IMPORT_TEXT_MAX)).ok).toBe(true)
    expect(checkImport('text', '🍝'.repeat(IMPORT_TEXT_MAX + 1)).ok).toBe(false)
  })
})

test('progress stages', () => {
  expect(importStages('link')).toEqual(['Читаем пост…', 'Раскладываем ингредиенты…', 'Смотрим видео — это может занять до минуты…'])
  expect(importStages('text')).toEqual(['Раскладываем ингредиенты…'])
})

test('the video stage of a link import shows after about 8 s', () => {
  const videoStage = importStages('link').length - 1
  expect(videoStage * IMPORT_STAGE_MS).toBeGreaterThanOrEqual(7000)
  expect(videoStage * IMPORT_STAGE_MS).toBeLessThanOrEqual(9000)
})

describe('importFailure', () => {
  const unavailable = new ApiError(503, 'unavailable', 'Instagram не отдал пост. Скопируйте текст подписи и вставьте его сюда.')
  const notRecipe = new ApiError(422, 'not_a_recipe', 'Не нашли в тексте рецепт — вставьте текст с ингредиентами')

  test('503 on a link switches to the text field with the server message', () => {
    expect(importFailure(unavailable, 'link')).toEqual({ kind: 'paste-text', message: unavailable.message })
  })

  test('everything else is a message by the field', () => {
    expect(importFailure(notRecipe, 'text')).toEqual({ kind: 'message', message: notRecipe.message })
    expect(importFailure(unavailable, 'text')).toEqual({ kind: 'message', message: unavailable.message })
  })
})
