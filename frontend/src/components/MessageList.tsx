import { useEffect, useLayoutEffect, useRef } from 'react'
import type { ChatMessage } from '../types'

const STICK_THRESHOLD_PX = 80

function formatTime(ts: number) {
  return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

interface Props {
  messages: ChatMessage[]
  me: string
}

export function MessageList({ messages, me }: Props) {
  const listRef = useRef<HTMLUListElement>(null)
  const stuckToBottom = useRef(true)

  useEffect(() => {
    const el = listRef.current
    if (!el) return
    const onScroll = () => {
      stuckToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < STICK_THRESHOLD_PX
    }
    el.addEventListener('scroll', onScroll, { passive: true })
    return () => el.removeEventListener('scroll', onScroll)
  }, [])

  // Follow new messages, unless the user has scrolled up to read history.
  useLayoutEffect(() => {
    const el = listRef.current
    const last = messages[messages.length - 1]
    if (el && (stuckToBottom.current || last?.userId === me)) {
      el.scrollTop = el.scrollHeight
    }
  }, [messages, me])

  return (
    <ul className="messages" ref={listRef} role="log" aria-label="Chat messages">
      {messages.length === 0 && <li className="empty">No messages yet. Say hello.</li>}
      {messages.map((m, i) => (
        <li
          key={m.clientMsgId ?? `${m.timestamp}-${i}`}
          className={[
            'message',
            `message-${m.type}`,
            m.userId === me ? 'mine' : '',
            m.pending ? 'pending' : '',
            m.failed ? 'failed' : '',
          ].join(' ')}
        >
          {m.type === 'chat' && <span className="author">{m.userId}</span>}
          <span className="content">{m.content}</span>
          <span className="meta">
            {m.failed ? 'not sent' : m.pending ? 'sending…' : formatTime(m.timestamp)}
          </span>
        </li>
      ))}
    </ul>
  )
}
