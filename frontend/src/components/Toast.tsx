export function Toast({ text }: { text: string | null }) {
  if (!text) return null
  return (
    <div className="toast" role="alert">
      {text}
    </div>
  )
}
