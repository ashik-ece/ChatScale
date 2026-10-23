import { describe, expect, it } from 'vitest'
import { backoffDelay } from './backoff'

describe('backoffDelay', () => {
  it('doubles from the base delay', () => {
    const none = () => 0
    expect([0, 1, 2, 3].map((a) => backoffDelay(a, none))).toEqual([500, 1000, 2000, 4000])
  })

  it('is capped at 30s before jitter', () => {
    expect(backoffDelay(20, () => 0)).toBe(30_000)
  })

  it('adds up to one second of jitter', () => {
    expect(backoffDelay(0, () => 0.999)).toBeCloseTo(500 + 999)
    expect(backoffDelay(20, () => 0.5)).toBe(30_500)
  })
})
