import { applyEvent, type ChatEvent, ChatStreamError, type ContentPart, parseEvent, type SavedIds } from "./mapper";
import { parseSSE } from "./sse";

/**
 * What assistant-ui's ChatModelAdapter.run yields: the whole message so far, not a delta. Once
 * the server says where it logged the turn, every snapshot carries the ids in `metadata.custom`,
 * so the next message knows what it follows.
 */
export interface Snapshot {
  content: ContentPart[];
  metadata?: { custom: Partial<SavedIds> };
}

/**
 * Folds a stream of events into snapshots. The real adapter feeds it `eventsFromSSE(...)`
 * and the mock feeds it `mockReply(...)`, so what the UI shows is the same code path either way.
 * A server `error` event throws; `done` ends quietly. Aborting stops pulling from the source.
 */
export async function* foldEvents(events: AsyncIterable<ChatEvent>, signal?: AbortSignal): AsyncGenerator<Snapshot> {
  let parts: ContentPart[] = [];
  let saved: SavedIds | undefined;
  for await (const event of events) {
    if (signal?.aborted) {
      return;
    }
    if (event.type === "done") {
      continue;
    }
    if (event.type === "saved") {
      saved = event.saved;
    }
    parts = applyEvent(parts, event);
    yield saved ? { content: parts, metadata: { custom: { ...saved } } } : { content: parts };
  }
}

/** A past turn rebuilt from the events it streamed; one that ended in an error keeps what came before. */
export interface Replayed {
  content: ContentPart[];
  error?: string;
}

/** Replays a logged turn's events into its message, the same folding the live stream uses. */
export function replayEvents(events: readonly ChatEvent[]): Replayed {
  let parts: ContentPart[] = [];
  for (const event of events) {
    try {
      parts = applyEvent(parts, event);
    } catch (e) {
      if (e instanceof ChatStreamError) {
        return { content: parts, error: e.message };
      }
      throw e;
    }
  }
  return { content: parts };
}

/** The chat events carried by an SSE response body; lines that are not valid events are skipped. */
export async function* eventsFromSSE(body: ReadableStream<Uint8Array>): AsyncGenerator<ChatEvent> {
  for await (const msg of parseSSE(body)) {
    const event = parseEvent(msg.data);
    if (event) {
      yield event;
    }
  }
}
