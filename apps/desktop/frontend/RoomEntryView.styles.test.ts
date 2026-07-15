import { readFileSync } from 'node:fs'

import { describe, expect, it } from 'vitest'

const css = readFileSync('src/App.css', 'utf8')

describe('RoomEntryView styles', () => {
  it('keeps a visible keyboard focus indicator for the invite code input', () => {
    const focusVisibleRule = css.match(/\.room-entry-invite-input__control:focus-visible\s*\{([^}]*)\}/)?.[1] ?? ''

    expect(focusVisibleRule).toMatch(/outline\s*:/)
  })
})
