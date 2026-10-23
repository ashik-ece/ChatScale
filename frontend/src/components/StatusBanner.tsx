import type { Status } from '../types'

interface Props {
  status: Status
  attempt: number
  onRetry: () => void
}

export function StatusBanner({ status, attempt, onRetry }: Props) {
  const label = {
    connecting: 'Connecting…',
    connected: 'Connected',
    reconnecting: `Reconnecting (attempt ${attempt}…)`,
    disconnected: 'Disconnected',
  }[status]

  return (
    <div className={`banner banner-${status}`} role="status" aria-live="polite">
      <span>{label}</span>
      {status === 'disconnected' && (
        <button type="button" onClick={onRetry}>
          Retry
        </button>
      )}
    </div>
  )
}
