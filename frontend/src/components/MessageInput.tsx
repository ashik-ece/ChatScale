import { useState, type FormEvent } from 'react'

interface Props {
  disabled: boolean
  onSend: (content: string) => void
}

export function MessageInput({ disabled, onSend }: Props) {
  const [value, setValue] = useState('')

  // Enter submits the form natively, so the button and the key share this path.
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (disabled || !value.trim()) return
    onSend(value)
    setValue('')
  }

  return (
    <form className="composer" onSubmit={submit}>
      <input
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder={disabled ? 'Waiting for connection…' : 'Send a message'}
        disabled={disabled}
        maxLength={200}
        autoComplete="off"
        aria-label="Message"
      />
      <button type="submit" disabled={disabled || !value.trim()}>
        Send
      </button>
    </form>
  )
}
