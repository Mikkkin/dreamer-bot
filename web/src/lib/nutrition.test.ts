import { describe, expect, test } from 'bun:test'
import type { NutritionAutoItem } from '../api/types'
import {
  DAILY_REFERENCE,
  EMPTY_NUTRITION,
  apiTenths,
  calorieShares,
  checkNutrition,
  coverageLine,
  dailyPercent,
  donutArcs,
  formatKcal,
  nutritionSource,
  scaleTenths,
  topContributors,
  decimalTenths,
  formatApiDecimal,
  formatTenths,
  nutritionDraft,
  parseTenthsInput,
  per100FromDish,
  perDish,
  perServing,
  roundDiv,
  switchNutritionMode,
  tenthsFromApi,
  type NutritionDraft,
} from './nutrition'

const limits = { dish_weight_max_g: 20000 }
const draft = (patch: Partial<NutritionDraft>): NutritionDraft => ({ ...EMPTY_NUTRITION, ...patch })

describe('parseTenthsInput mirrors domain.ParseTenths', () => {
  test.each([
    ['12', 120],
    ['12,5', 125],
    ['12.5', 125],
    ['0', 0],
    ['12.', 120],
    [' 7 ', 70],
    ['1 200', 12000],
  ])('%p → %p', (input, tenths) => {
    expect(parseTenthsInput(input)).toEqual({ ok: true, tenths })
  })

  test('empty is "not given"', () => {
    expect(parseTenthsInput('')).toEqual({ ok: true, tenths: null })
    expect(parseTenthsInput('  ')).toEqual({ ok: true, tenths: null })
  })

  test.each(['abc', '-1', '1.25', '1,2,3', '.5', '1e3', '123456'])('rejects %p', (input) => {
    expect(parseTenthsInput(input).ok).toBe(false)
  })

  test('whole-dish values may have more integer digits', () => {
    expect(parseTenthsInput('123456', 7)).toEqual({ ok: true, tenths: 1234560 })
  })
})

describe('formatting', () => {
  test('formatTenths / decimalTenths like the server', () => {
    expect(formatTenths(125)).toBe('12,5')
    expect(formatTenths(60)).toBe('6')
    expect(formatTenths(0)).toBe('0')
    expect(decimalTenths(125)).toBe('12.5')
    expect(decimalTenths(832)).toBe('83.2')
  })

  test('API decimals', () => {
    expect(apiTenths('83.2')).toBe(832)
    expect(apiTenths('150')).toBe(1500)
    expect(apiTenths('1.25')).toBeNull()
    expect(apiTenths('x')).toBeNull()
    expect(formatApiDecimal('12.5')).toBe('12,5')
    expect(tenthsFromApi({ kcal: '150', protein: '12.5', fat: '6', carbs: '10.4' })).toEqual({ kcal: 1500, protein: 125, fat: 60, carbs: 104 })
    expect(tenthsFromApi({ kcal: 'bad', protein: '1', fat: '1', carbs: '1' })).toBeNull()
  })
})

describe('the arithmetic matches domain.Nutrition', () => {
  const per100 = { kcal: 1500, protein: 125, fat: 60, carbs: 104 }

  test('roundDiv rounds half up', () => {
    expect(roundDiv(5, 2)).toBe(3)
    expect(roundDiv(4, 3)).toBe(1)
    expect(roundDiv(0, 7)).toBe(0)
  })

  test('the API.md example: 800 g, 4 servings', () => {
    expect(perDish(per100, 800)).toEqual({ kcal: 12000, protein: 1000, fat: 480, carbs: 832 })
    expect(perServing(per100, 800, 4)).toEqual({ kcal: 3000, protein: 250, fat: 120, carbs: 208 })
  })

  test('whole dish → per 100 g', () => {
    expect(per100FromDish({ kcal: 12000, protein: 1000, fat: 480, carbs: 832 }, 800)).toEqual(per100)
    // 1000 kcal in 730 g = 136.98… → 137.0
    expect(per100FromDish({ kcal: 10000, protein: 0, fat: 0, carbs: 0 }, 730).kcal).toBe(1370)
  })
})

describe('checkNutrition', () => {
  test('per 100 g with weight and the recipe servings', () => {
    const r = checkNutrition(draft({ kcal: '150', protein: '12,5', fat: '6', carbs: '10.4', weight: '800' }), limits, { servings: 4 })
    expect(r).toEqual({
      ok: true,
      input: { kcal: '150', protein: '12.5', fat: '6', carbs: '10.4', weight_g: 800 },
      per100: { kcal: 1500, protein: 125, fat: 60, carbs: 104 },
      dish: { kcal: 12000, protein: 1000, fat: 480, carbs: 832 },
      serving: { kcal: 3000, protein: 250, fat: 120, carbs: 208 },
    })
  })

  test('the whole dish is sent per 100 g', () => {
    const r = checkNutrition(draft({ mode: 'dish', kcal: '1 200', protein: '100', fat: '48', carbs: '83,2', weight: '800' }), limits)
    if (!r.ok) throw new Error('expected ok')
    expect(r.input).toEqual({ kcal: '150', protein: '12.5', fat: '6', carbs: '10.4', weight_g: 800 })
    expect(r.serving).toBeNull()
  })

  test('nothing entered sends null', () => {
    expect(checkNutrition(EMPTY_NUTRITION, limits)).toEqual({ ok: true, input: null, per100: null, dish: null, serving: null })
    expect(checkNutrition(draft({ mode: 'dish' }), limits)).toMatchObject({ ok: true, input: null })
  })

  test('a blank macro is unknown, not zero; the weight is optional', () => {
    const blank = 'Укажите — можно 0'
    expect(checkNutrition(draft({ kcal: '90' }), limits)).toEqual({ ok: false, errors: { protein: blank, fat: blank, carbs: blank } })
    const r = checkNutrition(draft({ kcal: '90', protein: '0', fat: '0', carbs: '0' }), limits)
    expect(r).toMatchObject({ ok: true, input: { kcal: '90', protein: '0', fat: '0', carbs: '0', weight_g: null }, dish: null })
  })

  test('whole-dish mode needs the weight', () => {
    expect(checkNutrition(draft({ mode: 'dish', kcal: '1200' }), limits)).toEqual({
      ok: false,
      errors: { weight_g: 'Укажите вес блюда — по нему считаем на 100 г' },
    })
  })

  test('ranges are checked per 100 g, after the conversion', () => {
    expect(checkNutrition(draft({ kcal: '901' }), limits)).toMatchObject({ ok: false, errors: { kcal: 'До 900 ккал на 100 г' } })
    expect(checkNutrition(draft({ protein: '100,1' }), limits)).toMatchObject({ ok: false, errors: { protein: 'До 100 г на 100 г' } })
    // 2000 kcal in 200 g is 1000 kcal per 100 g
    expect(checkNutrition(draft({ mode: 'dish', kcal: '2000', weight: '200' }), limits)).toMatchObject({
      ok: false,
      errors: { kcal: 'До 900 ккал на 100 г — проверьте вес блюда' },
    })
    expect(checkNutrition(draft({ mode: 'dish', kcal: '20000', protein: '0', fat: '0', carbs: '0', weight: '20000' }), limits).ok).toBe(true)
  })

  test('weight bounds', () => {
    expect(checkNutrition(draft({ kcal: '1', weight: '0' }), limits)).toMatchObject({ ok: false, errors: { weight_g: 'Вес блюда от 1 до 20000 г' } })
    expect(checkNutrition(draft({ kcal: '1', weight: '20001' }), limits).ok).toBe(false)
    expect(checkNutrition(draft({ kcal: '1', weight: '1,5' }), limits)).toMatchObject({ ok: false, errors: { weight_g: 'Целое число' } })
  })

  test('bad numbers are reported per field', () => {
    const r = checkNutrition(draft({ kcal: 'abc', fat: '1,25' }), limits)
    expect(r.ok).toBe(false)
    if (!r.ok) expect(Object.keys(r.errors).sort()).toEqual(['fat', 'kcal'])
  })
})

describe('switchNutritionMode', () => {
  test('converts the typed values when the weight is known', () => {
    const d = draft({ kcal: '150', protein: '12,5', fat: '', carbs: '10,4', weight: '800' })
    const dish = switchNutritionMode(d, 'dish', limits)
    expect(dish).toEqual({ ok: true, draft: { ...d, mode: 'dish', kcal: '1200', protein: '100', carbs: '83,2' } })
    if (!dish.ok) throw new Error('expected ok')
    expect(switchNutritionMode(dish.draft, 'per100', limits)).toEqual({ ok: true, draft: { ...d, kcal: '150', protein: '12,5', carbs: '10,4' } })
  })

  test('never reinterprets typed values without a weight', () => {
    expect(switchNutritionMode(draft({ kcal: '150' }), 'dish', limits)).toEqual({
      ok: false,
      message: 'Чтобы пересчитать, сначала укажите вес блюда',
    })
    expect(switchNutritionMode(draft({ mode: 'dish', kcal: '1200' }), 'per100', limits).ok).toBe(false)
    expect(switchNutritionMode(draft({ kcal: '150', weight: '0' }), 'dish', limits).ok).toBe(false)
  })

  test('refuses to convert broken values', () => {
    expect(switchNutritionMode(draft({ kcal: 'abc', weight: '500' }), 'dish', limits)).toEqual({
      ok: false,
      message: 'Сначала исправьте значения — потом пересчитаем',
    })
  })

  test('an empty form switches freely', () => {
    expect(switchNutritionMode(draft({ weight: '' }), 'dish', limits)).toEqual({ ok: true, draft: draft({ mode: 'dish' }) })
    expect(switchNutritionMode(draft({}), 'per100', limits)).toEqual({ ok: true, draft: draft({}) })
  })
})

test('nutritionDraft starts from the stored per-100 g values', () => {
  expect(
    nutritionDraft({
      per_100g: { kcal: '150', protein: '12.5', fat: '6', carbs: '10.4' },
      weight_g: 800,
      servings: null,
      per_dish: null,
      per_serving: null,
    }),
  ).toEqual({ mode: 'per100', kcal: '150', protein: '12,5', fat: '6', carbs: '10,4', weight: '800' })
  expect(nutritionDraft(null)).toEqual(EMPTY_NUTRITION)
})

describe('the КБЖУ card', () => {
  const t = { kcal: 5400, protein: 320, fat: 320, carbs: 150 }

  test('calorie shares: protein × 4, fat × 9, carbs × 4', () => {
    // 128 + 288 + 60 = 476
    const shares = calorieShares(t)
    expect(shares.protein).toBeCloseTo(128 / 476, 6)
    expect(shares.fat).toBeCloseTo(288 / 476, 6)
    expect(shares.carbs).toBeCloseTo(60 / 476, 6)
    expect(calorieShares({ kcal: 0, protein: 0, fat: 0, carbs: 0 })).toEqual({ protein: 0, fat: 0, carbs: 0 })
  })

  test('the daily reference of ТР ТС 022/2011', () => {
    expect(DAILY_REFERENCE).toEqual({ kcal: 25000, protein: 750, fat: 830, carbs: 3650 })
    expect(dailyPercent(t, 'kcal')).toBe(22)
    expect(dailyPercent(t, 'protein')).toBe(43)
    expect(dailyPercent({ ...t, fat: 1000 }, 'fat')).toBe(120)
  })

  test('whole kilocalories', () => {
    expect(formatKcal(5404)).toBe('540')
    expect(formatKcal(5405)).toBe('541')
  })

  test('the whole dish follows the scaler', () => {
    const dish = { kcal: 12000, protein: 1000, fat: 480, carbs: 832 }
    expect(scaleTenths(dish, 6, 4)).toEqual({ kcal: 18000, protein: 1500, fat: 720, carbs: 1248 })
    expect(scaleTenths(dish, 4, 4)).toBe(dish)
    expect(scaleTenths(dish, 1, 3)).toEqual({ kcal: 4000, protein: 333, fat: 160, carbs: 277 })
  })

  test('donut arcs leave gaps for the round caps and cover the ring', () => {
    const c = 100
    const arcs = donutArcs({ protein: 0.25, fat: 0.5, carbs: 0.25 }, c, 10, 2)
    expect(arcs).toEqual([
      { key: 'protein', start: 6, length: 13 },
      { key: 'fat', start: 31, length: 38 },
      { key: 'carbs', start: 81, length: 13 },
    ])
    // One macro alone is a full ring.
    expect(donutArcs({ protein: 0, fat: 1, carbs: 0 }, c, 10)).toEqual([{ key: 'fat', start: 0, length: 100 }])
    // A sliver becomes a dot, never a negative length.
    expect(donutArcs({ protein: 0.01, fat: 0.99, carbs: 0 }, c, 10)[0]).toEqual({ key: 'protein', start: 6, length: 0 })
  })

  const items: NutritionAutoItem[] = [
    { name: 'Спагетти', food: 'Макароны сухие', grams: 320, kcal: '1187.2', protein: '41.6', fat: '4.8', carbs: '240' },
    { name: 'Яйца', food: 'Яйцо куриное', grams: 200, kcal: '314', protein: '25.4', fat: '21.8', carbs: '1.4' },
    { name: 'Сыр', food: 'Пармезан', grams: 50, kcal: '196', protein: '17.9', fat: '13.9', carbs: '0' },
    { name: 'Бекон', food: 'Бекон', grams: 100, kcal: '417', protein: '13', fat: '40', carbs: '1.3' },
  ]

  test('top contributors to a macro', () => {
    expect(topContributors(items, 'fat')).toEqual({
      by: 'fat',
      list: [
        { name: 'Бекон', percent: 50 },
        { name: 'Яйца', percent: 27 },
        { name: 'Сыр', percent: 17 },
      ],
    })
    expect(topContributors(items, 'carbs', 2).list.map((c) => c.name)).toEqual(['Спагетти', 'Яйца'])
  })

  test('only calories per ingredient: ranked by calories', () => {
    const kcalOnly = items.map(({ name, food, grams, kcal }) => ({ name, food, grams, kcal }))
    expect(topContributors(kcalOnly, 'protein')).toMatchObject({ by: 'kcal', list: [{ name: 'Спагетти' }, { name: 'Бекон' }, { name: 'Яйца' }] })
    expect(topContributors([], 'fat')).toEqual({ by: 'kcal', list: [] })
  })

  test('the coverage line', () => {
    expect(coverageLine({ counted: 5, total: 6, missing: ['Гуанчале'], no_amount: [], skipped: ['Перец чёрный'] })).toBe(
      'Посчитано по 5 из 6 · нет: Гуанчале · перец чёрный не влияет',
    )
    expect(coverageLine({ counted: 3, total: 6, missing: ['А', 'Б'], no_amount: ['В'], skipped: ['Соль', 'Перец'] })).toBe(
      'Посчитано по 3 из 6 · нет: А, Б и ещё 1 · соль, перец не влияют',
    )
    expect(coverageLine({ counted: 4, total: 4, missing: [], no_amount: [], skipped: [] })).toBe('Посчитано по 4 из 4')
  })

  const auto = {
    per_100g: { kcal: '155.3', protein: '6', fat: '8.1', carbs: '14.2' },
    weight_g: 750,
    per_dish: { kcal: '1164.8', protein: '45', fat: '60.8', carbs: '106.5' },
    per_serving: null,
    coverage: { counted: 5, total: 6, missing: ['Гуанчале'], no_amount: [], skipped: [] },
    items,
  }

  test('the source: КБЖУ typed by hand wins over the estimate', () => {
    const manual = {
      per_100g: { kcal: '150', protein: '12.5', fat: '6', carbs: '10.4' },
      weight_g: 800,
      servings: 4,
      per_dish: { kcal: '1200', protein: '100', fat: '48', carbs: '83.2' },
      per_serving: { kcal: '300', protein: '25', fat: '12', carbs: '20.8' },
    }
    expect(nutritionSource(manual, auto)).toMatchObject({ kind: 'manual', weightG: 800, serving: { kcal: 3000 }, coverage: null, items: [] })
    expect(nutritionSource(null, auto)).toMatchObject({ kind: 'auto', per100: { kcal: 1553 }, dish: { kcal: 11648 }, serving: null, weightG: 750 })
    expect(nutritionSource(null, null)).toBeNull()
    expect(nutritionSource(null, undefined)).toBeNull()
  })
})
