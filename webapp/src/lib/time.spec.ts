import { describe, expect, it } from 'vitest'

import { formatCompactTimestamp } from './time'

describe('formatCompactTimestamp', () => {
  it('formats ISO timestamps for compact admin displays', () => {
    expect(formatCompactTimestamp('2026-03-22T10:00:00Z')).toBe('2026-03-22 10:00')
  })

  it('returns a fallback when the timestamp is missing', () => {
    expect(formatCompactTimestamp('')).toBe('--')
  })
})
