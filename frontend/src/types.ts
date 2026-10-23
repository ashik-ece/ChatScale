export type MessageType = 'chat' | 'system' | 'error'

export type Status = 'connecting' | 'connected' | 'reconnecting' | 'disconnected'

/** A message as it appears in the list, mirroring the Go message.Message envelope. */
export interface ChatMessage {
  type: MessageType
  userId: string
  roomId: string
  content: string
  timestamp: number
  nodeId?: string
  clientMsgId?: string
  /** Sent optimistically, not yet echoed by the server. */
  pending?: boolean
  /** The server rejected it, or the connection dropped before it was echoed. */
  failed?: boolean
}

/** Sent by the server to one client when its message is rejected. */
export interface ServerError {
  type: 'error'
  reason: string
  clientMsgId?: string
}

export function isServerError(data: ChatMessage | ServerError): data is ServerError {
  return data.type === 'error' && 'reason' in data
}
