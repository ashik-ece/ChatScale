import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MAX_ATTEMPTS } from '../lib/backoff'
import { NOTICE_MS, useWebSocket } from './useWebSocket'

class FakeWebSocket {
  static OPEN = 1
  static instances: FakeWebSocket[] = []

  readyState = 0
  sent: string[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null

  url: string

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }
  send(data: string) {
    this.sent.push(data)
  }
  close() {
    this.readyState = 3
    this.onclose?.()
  }

  // Test helpers: what the network would do.
  open() {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.()
  }
  drop() {
    this.readyState = 3
    this.onclose?.()
  }
  receive(obj: unknown) {
    this.onmessage?.({ data: JSON.stringify(obj) })
  }
}

const sockets = () => FakeWebSocket.instances
const last = () => sockets()[sockets().length - 1]

function setup() {
  return renderHook(() => useWebSocket({ roomId: 'room1', userId: 'alice' }))
}

beforeEach(() => {
  FakeWebSocket.instances = []
  vi.useFakeTimers()
  vi.spyOn(Math, 'random').mockReturnValue(0) // no jitter, so delays are exactly 500, 1000, 2000…
  vi.stubGlobal('WebSocket', FakeWebSocket)
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('connection lifecycle', () => {
  it('goes connecting -> connected and builds the room URL', () => {
    const { result } = setup()
    expect(result.current.status).toBe('connecting')
    expect(last().url).toContain('/ws/room1?user=alice')

    act(() => last().open())
    expect(result.current.status).toBe('connected')
  })

  it('reconnects with increasing delays, not rapid-fire', () => {
    const { result } = setup()
    act(() => last().open())

    act(() => last().drop())
    expect(result.current.status).toBe('reconnecting')
    expect(result.current.attempt).toBe(1)

    // Attempt 1 waits 500ms.
    act(() => vi.advanceTimersByTime(499))
    expect(sockets()).toHaveLength(1)
    act(() => vi.advanceTimersByTime(1))
    expect(sockets()).toHaveLength(2)

    // It fails again before opening: attempt 2 waits 1000ms.
    act(() => last().drop())
    expect(result.current.attempt).toBe(2)
    act(() => vi.advanceTimersByTime(999))
    expect(sockets()).toHaveLength(2)
    act(() => vi.advanceTimersByTime(1))
    expect(sockets()).toHaveLength(3)

    // Attempt 3 waits 2000ms.
    act(() => last().drop())
    expect(result.current.attempt).toBe(3)
    act(() => vi.advanceTimersByTime(1999))
    expect(sockets()).toHaveLength(3)
    act(() => vi.advanceTimersByTime(1))
    expect(sockets()).toHaveLength(4)
  })

  it('resets the backoff after a successful reconnect', () => {
    const { result } = setup()
    act(() => last().open())
    act(() => last().drop())
    act(() => vi.advanceTimersByTime(500))
    act(() => last().drop())
    act(() => vi.advanceTimersByTime(1000))
    act(() => last().open())
    expect(result.current.status).toBe('connected')
    expect(result.current.attempt).toBe(0)

    act(() => last().drop())
    act(() => vi.advanceTimersByTime(500)) // back to the base delay
    expect(sockets()).toHaveLength(4)
  })

  it('gives up after MAX_ATTEMPTS and reconnects on demand', () => {
    const { result } = setup()
    for (let i = 0; i < MAX_ATTEMPTS; i++) {
      act(() => last().drop())
      act(() => vi.advanceTimersByTime(60_000))
    }
    act(() => last().drop())
    expect(result.current.status).toBe('disconnected')

    const before = sockets().length
    act(() => vi.advanceTimersByTime(120_000))
    expect(sockets()).toHaveLength(before) // no more retries

    act(() => result.current.reconnect())
    expect(sockets()).toHaveLength(before + 1)
    act(() => last().open())
    expect(result.current.status).toBe('connected')
  })

  it('closes the socket and stops retrying on unmount', () => {
    const { unmount } = setup()
    act(() => last().open())
    const ws = last()
    unmount()
    expect(ws.readyState).toBe(3)
    act(() => vi.advanceTimersByTime(60_000))
    expect(sockets()).toHaveLength(1)
  })
})

describe('messages', () => {
  it('renders sends optimistically and does not duplicate them on echo', () => {
    const { result } = setup()
    act(() => last().open())

    act(() => result.current.sendMessage('hello'))
    expect(result.current.messages).toHaveLength(1)
    expect(result.current.messages[0]).toMatchObject({ content: 'hello', pending: true })

    const sent = JSON.parse(last().sent[0])
    expect(sent.clientMsgId).toBeTruthy()

    act(() =>
      last().receive({ type: 'chat', userId: 'alice', roomId: 'room1', content: 'hello', timestamp: 5, clientMsgId: sent.clientMsgId }),
    )
    expect(result.current.messages).toHaveLength(1)
    expect(result.current.messages[0]).toMatchObject({ pending: false, timestamp: 5 })
  })

  it('shows others messages and does not send while disconnected', () => {
    const { result } = setup()
    act(() => result.current.sendMessage('too early'))
    expect(result.current.messages).toHaveLength(0)

    act(() => last().open())
    act(() => last().receive({ type: 'chat', userId: 'bob', roomId: 'room1', content: 'hi', timestamp: 1 }))
    expect(result.current.messages[0].userId).toBe('bob')
  })

  it('drops a rate-limited message and shows a toast for 2 seconds', () => {
    const { result } = setup()
    act(() => last().open())
    act(() => result.current.sendMessage('spam'))
    const { clientMsgId } = JSON.parse(last().sent[0])

    act(() => last().receive({ type: 'error', reason: 'rate_limited', clientMsgId }))
    expect(result.current.messages).toHaveLength(0)
    expect(result.current.notice).toMatch(/too fast/)

    act(() => vi.advanceTimersByTime(NOTICE_MS - 1))
    expect(result.current.notice).not.toBeNull()
    act(() => vi.advanceTimersByTime(1))
    expect(result.current.notice).toBeNull()
  })

  it('marks pending messages failed when the connection drops', () => {
    const { result } = setup()
    act(() => last().open())
    act(() => result.current.sendMessage('lost'))
    act(() => last().drop())
    expect(result.current.messages[0]).toMatchObject({ pending: false, failed: true })
  })

  it('refuses messages over the gateway frame limit', () => {
    const { result } = setup()
    act(() => last().open())
    act(() => result.current.sendMessage('é'.repeat(250))) // 500 bytes
    expect(last().sent).toHaveLength(0)
    expect(result.current.notice).toMatch(/too long/)
  })
})
