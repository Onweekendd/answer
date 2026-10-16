import { applyEvent, type ChatEvent, type ContentPart, parseEvent } from "./mapper";
import { parseSSE } from "./sse";

/** What assistant-ui's ChatModelAdapter.run yields: the whole message so far, not a delta. */
export interface Snapshot {
  content: ContentPart[];
}

/**
 * Folds a stream of events into snapshots. The real adapter feeds it `eventsFromSSE(...)`
 * and the mock feeds it `mockReply(...)`, so what the UI shows is the same code path either way.
 * A server `error` event throws; `done` ends quietly. Aborting stops pulling from the source.
 */
export async function* foldEvents(events: AsyncIterable<ChatEvent>, signal?: AbortSignal): AsyncGenerator<Snapshot> {
  let parts: ContentPart[] = [];
  for await (const event of events) {
    if (signal?.aborted) {
      return;
    }
    if (event.type === "done") {
      continue;
    }
    parts = applyEvent(parts, event);
    yield { content: parts };
  }
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
