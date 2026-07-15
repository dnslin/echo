import { describe, expect, it } from 'vitest'

import { normalizeInviteCode } from './inviteCode'

describe('normalizeInviteCode', () => {
  it('removes whitespace and ASCII hyphens, then uppercases a complete invite code', () => {
    expect(normalizeInviteCode(' k7-m9 q2 ')).toEqual({ value: 'K7M9Q2', isComplete: true, isValid: true })
    expect(normalizeInviteCode('　k7-m9 q2　')).toEqual({ value: 'K7M9Q2', isComplete: true, isValid: true })
  })

  it.each([
    ['', { value: '', isComplete: false, isValid: true }],
    ['ABCDE', { value: 'ABCDE', isComplete: false, isValid: true }],
    ['ABCDEFG', { value: 'ABCDEFG', isComplete: false, isValid: false }],
    ['K7M9Q!', { value: 'K7M9Q', isComplete: false, isValid: false }],
    ['Ｋ7M9Q2', { value: '7M9Q2', isComplete: false, isValid: false }],
  ])('does not make %j a submit-ready invite code', (input, expected) => {
    expect(normalizeInviteCode(input)).toEqual(expected)
  })
})
