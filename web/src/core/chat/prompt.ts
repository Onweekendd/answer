/** The slice of a thread message we read; assistant-ui's ThreadMessage satisfies it structurally. */
export interface MessageLike {
  role: string
  content: readonly { type: string; text?: string }[]
}

/** The text the user last sent, with image parts left out; empty if there is none. */
export function lastUserText(messages: readonly MessageLike[]): string {
  const last = messages.findLast((m) => m.role === 'user')
  if (!last) return ''
  return last.content
    .flatMap((p) => (p.type === 'text' && typeof p.text === 'string' ? [p.text] : []))
    .join('\n')
    .trim()
}
