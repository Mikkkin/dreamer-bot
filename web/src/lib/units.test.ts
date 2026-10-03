import { describe, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import {
  BAD_AMOUNT,
  amountHundredths,
  chipOf,
  decimalOf,
  formatAmount,
  formatHundredths,
  formatQuantity,
  joinFraction,
  parseAmount,
  parseUnit,
  pluralFor,
  unitForms,
  unitLabel,
  unitOptionLabel,
  type UnitFormsTable,
} from './units'

interface UnitVector {
  amount: string
  unit: string
  label: string
  formatted: string
}

interface AmountVector {
  input: string
  amount: string | null
}

const NB = ' '

/** The examples of the build contract; the shared file below has these and more. */
const CONTRACT_VECTORS: UnitVector[] = [
  { amount: '1', unit: 'ч. л.', label: 'чайная ложка', formatted: `1${NB}чайная ложка` },
  { amount: '2', unit: 'ч. л.', label: 'чайные ложки', formatted: `2${NB}чайные ложки` },
  { amount: '5', unit: 'ч. л.', label: 'чайных ложек', formatted: `5${NB}чайных ложек` },
  { amount: '21', unit: 'ч. л.', label: 'чайная ложка', formatted: `21${NB}чайная ложка` },
  { amount: '0.5', unit: 'ч. л.', label: 'чайной ложки', formatted: `½${NB}чайной ложки` },
  { amount: '1.5', unit: 'ч. л.', label: 'чайной ложки', formatted: `1½${NB}чайной ложки` },
  { amount: '0.33', unit: 'стакан', label: 'стакана', formatted: `⅓${NB}стакана` },
  { amount: '2.5', unit: 'стакан', label: 'стакана', formatted: `2½${NB}стакана` },
  { amount: '3', unit: 'стакан', label: 'стакана', formatted: `3${NB}стакана` },
  { amount: '11', unit: 'стакан', label: 'стаканов', formatted: `11${NB}стаканов` },
  { amount: '2', unit: 'щепотка', label: 'щепотки', formatted: `2${NB}щепотки` },
  { amount: '5', unit: 'зубчик', label: 'зубчиков', formatted: `5${NB}зубчиков` },
  { amount: '0.5', unit: 'кг', label: 'кг', formatted: `½${NB}кг` },
  { amount: '1.25', unit: 'кг', label: 'кг', formatted: `1¼${NB}кг` },
  { amount: '0.3', unit: 'л', label: 'л', formatted: `0,3${NB}л` },
  { amount: '250', unit: 'г', label: 'г', formatted: `250${NB}г` },
  { amount: '1.5', unit: 'г', label: 'г', formatted: `1,5${NB}г` },
  { amount: '0.5', unit: 'шт', label: 'шт', formatted: `½${NB}шт` },
  { amount: '2', unit: '', label: '', formatted: '2' },
  { amount: '0.5', unit: '', label: '', formatted: '½' },
  { amount: '', unit: 'по вкусу', label: 'по вкусу', formatted: 'по вкусу' },
]

/** The vectors shared with the Go tests; a missing file fails the run instead of skipping it. */
function shared<T>(name: string): T[] {
  const path = fileURLToPath(new URL(`../../../internal/domain/testdata/${name}`, import.meta.url))
  return JSON.parse(readFileSync(path, 'utf8')) as T[]
}

const SHARED_UNITS = shared<UnitVector>('units.json')
const SHARED_AMOUNTS = shared<AmountVector>('amounts.json')

const formatCase = (v: UnitVector) => [`${v.amount || '—'} ${v.unit || '(bare)'}`, v] as const

describe('formatting matches Quantity.Format', () => {
  test.each([...CONTRACT_VECTORS, ...SHARED_UNITS].map(formatCase))('%s', (_, v) => {
    const amount = v.amount === '' ? null : v.amount
    const unit = v.unit === '' ? null : v.unit
    expect(formatQuantity(amount, unit)).toBe(v.formatted)
    expect(unitLabel(unit, amountHundredths(amount)) ?? '').toBe(v.label)
  })

  test('the shared vectors files are not empty', () => {
    expect(SHARED_UNITS.length).toBeGreaterThan(40)
    expect(SHARED_AMOUNTS.length).toBeGreaterThan(20)
  })

  test('Me.unit_forms wins over the built-in table', () => {
    const forms: UnitFormsTable = { стакан: { one: 'кружка', few: 'кружки', many: 'кружек', fraction: 'кружки' } }
    expect(formatQuantity('2', 'стакан', forms)).toBe(`2${NB}кружки`)
    // A code the server does not list falls back to the built-in words.
    expect(formatQuantity('2', 'ч. л.', forms)).toBe(`2${NB}чайные ложки`)
    expect(unitForms('л', forms)).toEqual({ one: 'л', few: 'л', many: 'л', fraction: 'л' })
  })

  test('decimal forces the comma (metric amounts converted by the scaler)', () => {
    expect(formatHundredths(150, 'л', null, { decimal: true })).toBe(`1,5${NB}л`)
    expect(formatAmount(150, 'л')).toBe('1½')
  })
})

describe('pluralFor', () => {
  test.each([
    [100, 'one'],
    [2100, 'one'],
    [10100, 'one'],
    [1100, 'many'],
    [11100, 'many'],
    [200, 'few'],
    [2200, 'few'],
    [10400, 'few'],
    [1200, 'many'],
    [1400, 'many'],
    [500, 'many'],
    [0, 'many'],
    [50, 'fraction'],
    [150, 'fraction'],
    [250, 'fraction'],
    [33, 'fraction'],
  ] as const)('%p hundredths → %p', (h, form) => {
    expect(pluralFor(h)).toBe(form)
  })
})

describe('parseAmount mirrors domain.ParseQuantityAmount', () => {
  const builtIn: AmountVector[] = [
    { input: '1/2', amount: '0.5' },
    { input: '1 1/2', amount: '1.5' },
    { input: '½', amount: '0.5' },
    { input: '1½', amount: '1.5' },
    { input: '0,5', amount: '0.5' },
    { input: '0.5', amount: '0.5' },
    { input: '1/3', amount: '0.33' },
    { input: '2/3', amount: '0.67' },
    { input: '1/8', amount: null },
  ]
  test.each([...builtIn, ...SHARED_AMOUNTS].map((v) => [v.input, v] as const))('%p', (_, v) => {
    const r = parseAmount(v.input)
    if (v.amount === null) expect(r.ok).toBe(false)
    else expect(r.ok && decimalOf(r.hundredths)).toBe(v.amount)
  })

  test('the words people type', () => {
    expect(parseAmount('пол')).toEqual({ ok: true, hundredths: 50 })
    expect(parseAmount(' Полторы ')).toEqual({ ok: true, hundredths: 150 })
    expect(parseAmount('полтора')).toEqual({ ok: true, hundredths: 150 })
  })

  test('the fraction slash and a vulgar fraction after a space', () => {
    expect(parseAmount('1⁄2')).toEqual({ ok: true, hundredths: 50 })
    expect(parseAmount('2 ¾')).toEqual({ ok: true, hundredths: 275 })
  })

  test('messages', () => {
    expect(parseAmount('1/7')).toEqual({ ok: false, message: BAD_AMOUNT })
    expect(parseAmount('0')).toEqual({ ok: false, message: 'Количество должно быть больше нуля' })
    expect(parseAmount('200000')).toEqual({ ok: false, message: 'Слишком большое количество' })
  })
})

describe('canonical decimals', () => {
  test.each([
    [150, '1.5'],
    [33, '0.33'],
    [200, '2'],
    [1, '0.01'],
    [10, '0.1'],
  ])('%p ↔ %p', (h, s) => {
    expect(decimalOf(h)).toBe(s)
    expect(amountHundredths(s)).toBe(h)
  })

  test('anything else is not an amount', () => {
    expect(amountHundredths(null)).toBeNull()
    expect(amountHundredths('1,5')).toBeNull()
    expect(amountHundredths('0')).toBeNull()
    expect(amountHundredths('1.234')).toBeNull()
  })
})

describe('fraction chips join the whole part', () => {
  test.each([
    ['', '½', '½'],
    ['1', '½', '1½'],
    ['1½', '¼', '1¼'],
    ['1,5', '¾', '1¾'],
    ['2 1/3', '⅔', '2⅔'],
    ['1½', '½', '1'],
    ['½', '½', ''],
    ['abc', '⅓', '⅓'],
  ] as const)('%p + %p → %p', (raw, chip, want) => {
    expect(joinFraction(raw, chip)).toBe(want)
  })

  test('chipOf tells the chip in use', () => {
    expect(chipOf('1½')).toBe('½')
    expect(chipOf('0,25')).toBe('¼')
    expect(chipOf('2')).toBeNull()
    expect(chipOf('1,2')).toBeNull()
  })
})

describe('units as typed', () => {
  test.each([
    ['ч.л.', 'ч. л.'],
    ['Ч Л', 'ч. л.'],
    ['чл', 'ч. л.'],
    ['чайных ложек', 'ч. л.'],
    ['ст. ложка', 'ст. л.'],
    ['гр.', 'г'],
    ['граммов', 'г'],
    ['шт.', 'шт'],
    ['штук', 'шт'],
    ['зуб', 'зубчик'],
    ['уп', 'упаковка'],
    ['литра', 'л'],
    ['По вкусу', 'по вкусу'],
    ['щёпотки', 'щепотка'],
    ['ведро', null],
    ['', ''],
  ] as const)('%p → %p', (raw, code) => {
    expect(parseUnit(raw)).toBe(code)
  })

  test('the picker declines for the typed amount', () => {
    expect(unitOptionLabel('ч. л.', '2')).toBe('чайные ложки')
    expect(unitOptionLabel('стакан', '1½')).toBe('стакана')
    expect(unitOptionLabel('стакан', '5')).toBe('стаканов')
    expect(unitOptionLabel('ч. л.', '')).toBe('чайная ложка')
    expect(unitOptionLabel('г', '2')).toBe('г')
  })
})
