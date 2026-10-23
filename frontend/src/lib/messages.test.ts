import { describe, expect, it } from 'vitest'
import type { ChatMessage } from '../types'
import { MAX_MESSAGES, failAllPending, receiveMessage, rejectPending } from './messages'

const msg = (over: Partial<ChatMessage> = {}): ChatMessage => ({
  type: 'chat',
  userId: 'alice',
  roomId: 'r',
  content: 'hi',
  timestamp: 1,
  ...over,
})

describe('receiveMessage', () => {
  it('appends messages from others', () => {
    expect(receiveMessage([msg()], msg({ content: 'two' }))).toHaveLength(2)
  })

  it('replaces the pending copy instead of duplicating it', () => {
    const list = [msg({ clientMsgId: 'a', pending: true })]
    const out = receiveMessage(list, msg({ clientMsgId: 'a', timestamp: 9 }))
    expect(out).toHaveLength(1)
    expect(out[0]).toMatchObject({ clientMsgId: 'a', timestamp: 9, pending: false })
  })

  it('appends a message whose id has no pending copy (e.g. from another tab)', () => {
    expect(receiveMessage([], msg({ clientMsgId: 'zzz' }))).toHaveLength(1)
  })

  it('keeps only the newest MAX_MESSAGES', () => {
    const full = Array.from({ length: MAX_MESSAGES }, (_, i) => msg({ content: String(i) }))
    const out = receiveMessage(full, msg({ content: 'new' }))
    expect(out).toHaveLength(MAX_MESSAGES)
    expect(out[out.length - 1].content).toBe('new')
    expect(out[0].content).toBe('1')
  })
})

describe('rejectPending', () => {
  const list = [msg({ clientMsgId: 'a', pending: true }), msg({ clientMsgId: 'b', pending: true })]

  it('removes a rate-limited message', () => {
    expect(rejectPending(list, 'a', 'rate_limited').map((m) => m.clientMsgId)).toEqual(['b'])
  })

  it('marks other rejections failed', () => {
    const out = rejectPending(list, 'a', 'timed_out')
    expect(out[0]).toMatchObject({ pending: false, failed: true })
    expect(out[1].pending).toBe(true)
  })
})

describe('failAllPending', () => {
  it('fails every pending message and returns the same list when none are pending', () => {
    const none = [msg()]
    expect(failAllPending(none)).toBe(none)
    expect(failAllPending([msg({ pending: true })])[0]).toMatchObject({ pending: false, failed: true })
  })
})
