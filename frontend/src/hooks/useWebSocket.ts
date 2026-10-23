import { useCallback, useEffect, useRef, useState } from 'react'
import { MAX_ATTEMPTS, backoffDelay } from '../lib/backoff'
import {
  MAX_CONTENT_BYTES,
  byteLength,
  failAllPending,
  makeId,
  receiveMessage,
  rejectPending,
} from '../lib/messages'
import { isServerError, type ChatMessage, type ServerError, type Status } from '../types'

export const NOTICE_MS = 2000

const NOTICES: Record<string, string> = {
  rate_limited: 'Slow down: you are sending messages too fast',
  timed_out: 'You are timed out',
  bad_command: 'Unknown or malformed command',
}

interface Options {
  roomId: string
  userId: string
  isMod?: boolean
}

export function useWebSocket({ roomId, userId, isMod = false }: Options) {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [status, setStatus] = useState<Status>('connecting')
  const [attempt, setAttempt] = useState(0)
  const [notice, setNotice] = useState<string | null>(null)
  const [retryToken, setRetryToken] = useState(0)

  const wsRef = useRef<WebSocket | null>(null)
  const noticeTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const showNotice = useCallback((text: string) => {
    setNotice(text)
    clearTimeout(noticeTimer.current)
    noticeTimer.current = setTimeout(() => setNotice(null), NOTICE_MS)
  }, [])

  useEffect(() => () => clearTimeout(noticeTimer.current), [])

  useEffect(() => {
    const scheme = location.protocol === 'https:' ? 'wss' : 'ws'
    const query = new URLSearchParams({ user: userId })
    if (isMod) query.set('mod', 'true')
    const url = `${scheme}://${location.host}/ws/${encodeURIComponent(roomId)}?${query}`

    let disposed = false
    let failures = 0
    let timer: ReturnType<typeof setTimeout> | undefined

    const connect = () => {
      setStatus(failures === 0 ? 'connecting' : 'reconnecting')
      const ws = new WebSocket(url)
      wsRef.current = ws

      ws.onopen = () => {
        failures = 0
        setAttempt(0)
        setStatus('connected')
      }

      ws.onmessage = (ev) => {
        let data: ChatMessage | ServerError
        try {
          data = JSON.parse(ev.data as string)
        } catch {
          return
        }
        if (isServerError(data)) {
          const reason = data.reason
          if (data.clientMsgId) {
            const id = data.clientMsgId
            setMessages((m) => rejectPending(m, id, reason))
          }
          showNotice(NOTICES[reason] ?? 'Message rejected')
          return
        }
        setMessages((m) => receiveMessage(m, data))
      }

      // A failed connection fires onerror and then onclose, so all retry
      // scheduling lives in onclose to avoid scheduling twice.
      ws.onerror = () => {}

      ws.onclose = () => {
        if (disposed || wsRef.current !== ws) return
        wsRef.current = null
        setMessages(failAllPending)

        if (failures >= MAX_ATTEMPTS) {
          setStatus('disconnected')
          return
        }
        const delay = backoffDelay(failures)
        failures += 1
        setAttempt(failures)
        setStatus('reconnecting')
        timer = setTimeout(connect, delay)
      }
    }

    connect()

    return () => {
      disposed = true
      clearTimeout(timer)
      const ws = wsRef.current
      wsRef.current = null
      ws?.close()
    }
  }, [roomId, userId, isMod, retryToken, showNotice])

  const sendMessage = useCallback(
    (content: string) => {
      const text = content.trim()
      const ws = wsRef.current
      if (!text || !ws || ws.readyState !== WebSocket.OPEN) return
      if (byteLength(text) > MAX_CONTENT_BYTES) {
        showNotice('Message is too long')
        return
      }

      const clientMsgId = makeId()
      // Mod commands are never echoed back as chat, so don't show them as pending.
      if (!(isMod && text.startsWith('!'))) {
        setMessages((m) => [
          ...m,
          { type: 'chat', userId, roomId, content: text, timestamp: Date.now(), clientMsgId, pending: true },
        ])
      }
      ws.send(JSON.stringify({ type: 'chat', content: text, clientMsgId }))
    },
    [isMod, roomId, userId, showNotice],
  )

  /** Manual retry after the hook has given up. */
  const reconnect = useCallback(() => setRetryToken((n) => n + 1), [])

  return { messages, sendMessage, status, attempt, notice, reconnect }
}
