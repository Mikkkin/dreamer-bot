import { describe, expect, test } from 'bun:test'
import {
  EMPTY_NUTRITION,
  apiTenths,
  checkNutrition,
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

const limits = { dish_weight_max_g: 20000, servings_max: 50 }
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
  test('per 100 g with weight and servings', () => {
    const r = checkNutrition(draft({ kcal: '150', protein: '12,5', fat: '6', carbs: '10.4', weight: '800', servings: '4' }), limits)
    expect(r).toEqual({
      ok: true,
      input: { kcal: '150', protein: '12.5', fat: '6', carbs: '10.4', weight_g: 800, servings: 4 },
      per100: { kcal: 1500, protein: 125, fat: 60, carbs: 104 },
      dish: { kcal: 12000, protein: 1000, fat: 480, carbs: 832 },
      serving: { kcal: 3000, protein: 250, fat: 120, carbs: 208 },
    })
  })

  test('the whole dish is sent per 100 g', () => {
    const r = checkNutrition(draft({ mode: 'dish', kcal: '1 200', protein: '100', fat: '48', carbs: '83,2', weight: '800' }), limits)
    if (!r.ok) throw new Error('expected ok')
    expect(r.input).toEqual({ kcal: '150', protein: '12.5', fat: '6', carbs: '10.4', weight_g: 800, servings: null })
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
    expect(r).toMatchObject({ ok: true, input: { kcal: '90', protein: '0', fat: '0', carbs: '0', weight_g: null, servings: null }, dish: null })
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

  test('weight and servings bounds', () => {
    expect(checkNutrition(draft({ kcal: '1', weight: '0' }), limits)).toMatchObject({ ok: false, errors: { weight_g: 'Вес блюда от 1 до 20000 г' } })
    expect(checkNutrition(draft({ kcal: '1', weight: '20001' }), limits).ok).toBe(false)
    expect(checkNutrition(draft({ kcal: '1', weight: '1,5' }), limits)).toMatchObject({ ok: false, errors: { weight_g: 'Целое число' } })
    expect(checkNutrition(draft({ kcal: '1', servings: '51' }), limits)).toMatchObject({ ok: false, errors: { servings: 'Порций от 1 до 50' } })
    expect(checkNutrition(draft({ kcal: '1', servings: '0' }), limits).ok).toBe(false)
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
  ).toEqual({ mode: 'per100', kcal: '150', protein: '12,5', fat: '6', carbs: '10,4', weight: '800', servings: '' })
  expect(nutritionDraft(null)).toEqual(EMPTY_NUTRITION)
})
