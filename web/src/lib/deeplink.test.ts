import { describe, expect, test } from 'bun:test'
import { parseDeepLink } from './deeplink'

describe('parseDeepLink', () => {
  test.each([
    ['?wish=12', null, { kind: 'wish', id: 12 }],
    ['?recipe=4', null, { kind: 'recipe', id: 4 }],
    ['wish=7', null, { kind: 'wish', id: 7 }],
    ['?wish=12&recipe=4', null, { kind: 'wish', id: 12 }],
    ['', 'w_42', { kind: 'wish', id: 42 }],
    ['', 'r_9', { kind: 'recipe', id: 9 }],
    ['?recipe=3', 'w_42', { kind: 'recipe', id: 3 }],
    ['?shopping=1', null, { kind: 'shopping' }],
    ['?shopping=1&recipe=4', null, { kind: 'recipe', id: 4 }],
    ['?shopping=1', 'r_9', { kind: 'shopping' }],
  ] as const)('%p / %p', (search, start, want) => {
    expect(parseDeepLink(search, start)).toEqual(want)
  })

  test.each([
    ['', null],
    ['?wish=', null],
    ['?wish=0', null],
    ['?wish=-1', null],
    ['?wish=1e3', null],
    ['?wish=12abc', null],
    ['?wish=%2012', null],
    ['?wish=0012', null],
    ['?wish=99999999999999999999', null],
    ['?recipe=javascript:alert(1)', null],
    ['?shopping=0', null],
    ['?shopping=true', null],
    ['?shopping=', null],
    ['?shopping=1x', null],
    ['', 'x_1'],
    ['', 'w_'],
    ['', 'w_1_2'],
    ['', 'shopping'],
  ])('ignores %p / %p', (search, start) => {
    expect(parseDeepLink(search, start)).toBeNull()
  })
})
