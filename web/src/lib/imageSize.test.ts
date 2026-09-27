import { describe, expect, test } from 'bun:test'
import { MAX_PIXELS, MAX_WIDTH, fitImage } from './imageSize'

describe('fitImage (same rule as the server "full" variant)', () => {
  test('an iPhone screenshot stays untouched', () => {
    expect(fitImage(1170, 2532)).toEqual({ width: 1170, height: 2532, scaled: false })
  })

  test('a 12 MP camera photo is limited by width', () => {
    expect(fitImage(4000, 3000)).toEqual({ width: 1600, height: 1200, scaled: true })
  })

  test('a very tall screenshot is limited by the pixel budget and keeps its width as far as possible', () => {
    const got = fitImage(1170, 12000)
    expect(got.scaled).toBe(true)
    expect(got.width * got.height).toBeLessThanOrEqual(MAX_PIXELS)
    expect(got.width).toBeGreaterThan(1070)
    expect(Math.abs(got.width / got.height - 1170 / 12000)).toBeLessThan(0.001)
  })

  test('exact limits are not scaled', () => {
    expect(fitImage(1600, 7500).scaled).toBe(false)
    expect(fitImage(1601, 100)).toEqual({ width: 1600, height: 100, scaled: true })
  })

  test('portrait photos wider than the limit', () => {
    const got = fitImage(3024, 4032)
    expect(got).toEqual({ width: 1600, height: 2133, scaled: true })
    expect(got.width).toBeLessThanOrEqual(MAX_WIDTH)
  })

  test('tiny images are kept', () => {
    expect(fitImage(1, 1)).toEqual({ width: 1, height: 1, scaled: false })
  })

  test('non-positive sizes are rejected', () => {
    expect(() => fitImage(0, 10)).toThrow(RangeError)
    expect(() => fitImage(10, Number.NaN)).toThrow(RangeError)
  })
})
