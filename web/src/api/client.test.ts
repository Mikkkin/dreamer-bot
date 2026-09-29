import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { ApiClient } from './client'
import { ApiError } from './errors'

interface Call {
  method: string
  url: string
  body: unknown
  auth: string | null
}

let calls: Call[] = []
let reply: { status: number; body: unknown } = { status: 200, body: {} }
const realFetch = globalThis.fetch

beforeEach(() => {
  calls = []
  reply = { status: 200, body: {} }
  globalThis.fetch = (async (input: string | URL | Request, init?: RequestInit) => {
    const headers = new Headers(init?.headers)
    calls.push({
      method: init?.method ?? 'GET',
      url: String(input),
      body: typeof init?.body === 'string' ? (JSON.parse(init.body) as unknown) : undefined,
      auth: headers.get('Authorization'),
    })
    const text = reply.body === undefined ? '' : JSON.stringify(reply.body)
    return new Response(text, { status: reply.status })
  }) as typeof fetch
})

afterEach(() => {
  globalThis.fetch = realFetch
})

const failures: ApiError[] = []
const api = new ApiClient('query_id=x&hash=y', (e) => failures.push(e))

function last(): Call {
  const c = calls.at(-1)
  if (!c) throw new Error('no request was made')
  return c
}

describe('new endpoints hit the documented paths', () => {
  test('savings', async () => {
    reply.body = { savings: [{ id: 5 }] }
    expect(await api.savings(12)).toEqual([{ id: 5 }] as never)
    expect(last()).toMatchObject({ method: 'GET', url: '/api/wishes/12/savings' })

    reply = { status: 201, body: { id: 6 } }
    await api.addSaving(12, { amount: { amount: '5000', currency: 'RUB' }, note: 'с зарплаты' })
    expect(last()).toMatchObject({
      method: 'POST',
      url: '/api/wishes/12/savings',
      body: { amount: { amount: '5000', currency: 'RUB' }, note: 'с зарплаты' },
    })

    reply = { status: 204, body: undefined }
    await api.deleteSaving(12, 6)
    expect(last()).toMatchObject({ method: 'DELETE', url: '/api/wishes/12/savings/6' })
  })

  test('recipe tags', async () => {
    reply.body = { tags: [] }
    await api.recipeTags()
    expect(last()).toMatchObject({ method: 'GET', url: '/api/recipe-tags' })
    await api.createRecipeTag({ kind: 'cuisine', name: 'Грузинская', emoji: '🍢' })
    expect(last()).toMatchObject({ method: 'POST', url: '/api/recipe-tags', body: { kind: 'cuisine', name: 'Грузинская', emoji: '🍢' } })
    await api.updateRecipeTag(3, { emoji: '🥙' })
    expect(last()).toMatchObject({ method: 'PATCH', url: '/api/recipe-tags/3', body: { emoji: '🥙' } })
    await api.deleteRecipeTag(3)
    expect(last()).toMatchObject({ method: 'DELETE', url: '/api/recipe-tags/3' })
  })

  test('cooking and rating', async () => {
    await api.cookRecipe(4, null)
    expect(last()).toMatchObject({ method: 'POST', url: '/api/recipes/4/cooks', body: {} })
    await api.cookRecipe(4, { stars: 5, comment: '' })
    expect(last()).toMatchObject({ body: { stars: 5, comment: '' } })
    reply.body = { cooks: [] }
    await api.cooks(4)
    expect(last()).toMatchObject({ method: 'GET', url: '/api/recipes/4/cooks' })
    await api.rateCook(4, 17, { stars: 4, comment: 'Вкусно' })
    expect(last()).toMatchObject({ method: 'PUT', url: '/api/recipes/4/cooks/17/rating', body: { stars: 4, comment: 'Вкусно' } })
    await api.deleteCook(4, 17)
    expect(last()).toMatchObject({ method: 'DELETE', url: '/api/recipes/4/cooks/17' })
  })

  test('shopping', async () => {
    reply.body = { items: [] }
    await api.addRecipeToShopping(4, null)
    expect(last()).toMatchObject({ method: 'POST', url: '/api/recipes/4/shopping', body: {} })
    await api.addRecipeToShopping(4, [0, 2])
    expect(last()).toMatchObject({ body: { positions: [0, 2] } })
    await api.shopping()
    expect(last()).toMatchObject({ method: 'GET', url: '/api/shopping' })
    await api.addShopping([{ name: 'Хлеб', amount: null, unit: null }])
    expect(last()).toMatchObject({ method: 'POST', url: '/api/shopping', body: { items: [{ name: 'Хлеб', amount: null, unit: null }] } })
    await api.updateShopping(31, { checked: true })
    expect(last()).toMatchObject({ method: 'PATCH', url: '/api/shopping/31', body: { checked: true } })
    await api.deleteShopping(31)
    expect(last()).toMatchObject({ method: 'DELETE', url: '/api/shopping/31' })
    reply.body = { removed: 3 }
    expect(await api.clearCheckedShopping()).toBe(3)
    expect(last()).toMatchObject({ method: 'POST', url: '/api/shopping/clear-checked' })
    reply.body = { stores: [] }
    await api.stores()
    expect(last()).toMatchObject({ method: 'GET', url: '/api/stores' })
  })

  test('ВкусВилл cart', async () => {
    reply.body = { matches: [] }
    await api.vkusvillMatch(null)
    expect(last()).toMatchObject({ method: 'POST', url: '/api/shopping/vkusvill/match', body: {} })
    await api.vkusvillMatch([31, 32])
    expect(last()).toMatchObject({ body: { item_ids: [31, 32] } })
    reply.body = { url: 'https://vkusvill.ru/?share_basket=1', estimated_total: null }
    await api.vkusvillCart([{ xml_id: 173, quantity: '2' }])
    expect(last()).toMatchObject({ method: 'POST', url: '/api/shopping/vkusvill/cart', body: { lines: [{ xml_id: 173, quantity: '2' }] } })
  })

  test('ВкусВилл being down is "unavailable"', async () => {
    reply = { status: 503, body: { error: { code: 'unavailable', message: 'ВкусВилл не отвечает' } } }
    const err = await api.vkusvillMatch(null).catch((e: unknown) => e)
    expect((err as ApiError).code).toBe('unavailable')
    expect(failures.some((f) => f.code === 'unavailable')).toBe(false)
  })

  test('every request carries the launch data', async () => {
    reply.body = { items: [] }
    await api.shopping()
    expect(last().auth).toBe('tma query_id=x&hash=y')
  })
})

describe('ids and positions are validated before any request', () => {
  test.each([0, -1, 1.5, Number.NaN, Number.MAX_SAFE_INTEGER + 1])('rejects id %p', async (id) => {
    await expect((async () => api.deleteShopping(id))()).rejects.toBeInstanceOf(ApiError)
    await expect((async () => api.rateCook(4, id, { stars: 5, comment: '' }))()).rejects.toBeInstanceOf(ApiError)
    expect(calls).toHaveLength(0)
  })

  test('rejects negative or fractional ingredient positions', async () => {
    await expect(api.addRecipeToShopping(4, [0, -1])).rejects.toBeInstanceOf(ApiError)
    await expect(api.addRecipeToShopping(4, [0.5])).rejects.toBeInstanceOf(ApiError)
    expect(calls).toHaveLength(0)
  })
})

test('an error envelope becomes an ApiError with the field', async () => {
  reply = { status: 400, body: { error: { code: 'validation', message: 'копим в RUB — укажите сумму в этой валюте', field: 'currency' } } }
  const err = await api.addSaving(12, { amount: { amount: '5', currency: 'EUR' }, note: '' }).catch((e: unknown) => e)
  expect(err).toBeInstanceOf(ApiError)
  expect((err as ApiError).field).toBe('currency')
  expect((err as ApiError).message).toBe('Копим в RUB — укажите сумму в этой валюте')
})

test('match item ids are validated too', async () => {
  await expect(api.vkusvillMatch([31, 0])).rejects.toBeInstanceOf(ApiError)
  expect(calls).toHaveLength(0)
})
