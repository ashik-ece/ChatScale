import type { ChatMessage } from '../types'

export const MAX_MESSAGES = 500

// The gateway closes connections that send frames over 512 bytes; keep well under.
export const MAX_CONTENT_BYTES = 400

const trim = (list: ChatMessage[]) => (list.length > MAX_MESSAGES ? list.slice(-MAX_MESSAGES) : list)

/** Adds a message from the server, replacing the sender's own pending copy if there is one. */
export function receiveMessage(list: ChatMessage[], incoming: ChatMessage): ChatMessage[] {
  const id = incoming.clientMsgId
  if (id) {
    const i = list.findIndex((m) => m.clientMsgId === id && m.pending)
    if (i !== -1) {
      const next = list.slice()
      next[i] = { ...incoming, pending: false }
      return next
    }
  }
  return trim([...list, incoming])
}

/** A rate-limited message is dropped from the list; any other rejection marks it failed. */
export function rejectPending(list: ChatMessage[], clientMsgId: string, reason: string): ChatMessage[] {
  if (reason === 'rate_limited') {
    return list.filter((m) => !(m.clientMsgId === clientMsgId && m.pending))
  }
  return list.map((m) => (m.clientMsgId === clientMsgId && m.pending ? { ...m, pending: false, failed: true } : m))
}

export function failAllPending(list: ChatMessage[]): ChatMessage[] {
  if (!list.some((m) => m.pending)) return list
  return list.map((m) => (m.pending ? { ...m, pending: false, failed: true } : m))
}

export function byteLength(s: string): number {
  return new TextEncoder().encode(s).length
}

/** crypto.randomUUID needs a secure context, which http://<lan-ip> is not. */
export function makeId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}
