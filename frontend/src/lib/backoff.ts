export const BASE_DELAY_MS = 500
export const MAX_DELAY_MS = 30_000
export const JITTER_MS = 1000
export const MAX_ATTEMPTS = 10

/** Exponential backoff with jitter. `attempt` counts from 0. */
export function backoffDelay(
  attempt: number,
  rng: () => number = Math.random,
  base = BASE_DELAY_MS,
  max = MAX_DELAY_MS,
): number {
  return Math.min(base * 2 ** attempt, max) + rng() * JITTER_MS
}
