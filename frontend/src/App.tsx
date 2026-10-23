import { MessageInput } from './components/MessageInput'
import { MessageList } from './components/MessageList'
import { StatusBanner } from './components/StatusBanner'
import { Toast } from './components/Toast'
import { useWebSocket } from './hooks/useWebSocket'

const params = new URLSearchParams(location.search)
const roomId = params.get('room') || 'lobby'
const isMod = params.get('mod') === 'true'
const userId = params.get('user') || sessionUserId()

// Per-tab identity, so two tabs are two users with separate rate limits. There
// is no auth yet; the gateway trusts whatever is sent here.
function sessionUserId(): string {
  const key = 'chatscale:userId'
  try {
    const existing = sessionStorage.getItem(key)
    if (existing) return existing
    const id = `user-${Math.random().toString(36).slice(2, 6)}`
    sessionStorage.setItem(key, id)
    return id
  } catch {
    return `user-${Math.random().toString(36).slice(2, 6)}`
  }
}

export default function App() {
  const { messages, sendMessage, status, attempt, notice, reconnect } = useWebSocket({
    roomId,
    userId,
    isMod,
  })

  return (
    <>
      <StatusBanner status={status} attempt={attempt} onRetry={reconnect} />
      <main className="chat">
        <header>
          <h1># {roomId}</h1>
          <span className="who">
            {userId}
            {isMod && ' (mod)'}
          </span>
        </header>
        <MessageList messages={messages} me={userId} />
        <Toast text={notice} />
        <MessageInput disabled={status !== 'connected'} onSend={sendMessage} />
      </main>
    </>
  )
}
