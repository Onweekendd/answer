import {
  CompositeAttachmentAdapter,
  SimpleImageAttachmentAdapter,
  useLocalRuntime,
  type ChatModelAdapter,
  type ChatModelRunResult,
} from '@assistant-ui/react'
import { lastUserText } from '@/core/chat/prompt'
import { mockReply } from '@/core/chat/mock'
import { foldEvents } from '@/core/chat/stream'

/**
 * The chat adapter. For now it plays a scripted reply (mockReply); once the Go chat endpoint
 * exists only the event source changes, to `eventsFromSSE(response.body)`. The folding into
 * snapshots, and everything the UI shows, stays the same.
 */
const adapter: ChatModelAdapter = {
  async *run({ messages, abortSignal }) {
    // The cast is the one place our plain types meet the library's JSON types: every value in
    // a snapshot came from JSON.parse (or the mock's literals), so it is JSON by construction.
    for await (const snapshot of foldEvents(mockReply(lastUserText(messages), { signal: abortSignal }), abortSignal)) {
      yield snapshot as ChatModelRunResult
    }
  },
}

/** Images are read in the browser for now; the real flow uploads each to /file and sends the url. */
const attachments = new CompositeAttachmentAdapter([new SimpleImageAttachmentAdapter()])

export function useChatRuntime() {
  return useLocalRuntime(adapter, { adapters: { attachments } })
}
