export interface SSEMessage {
  event: string;
  data: string;
}

/**
 * Reads a Server-Sent Events body. Events arrive split at arbitrary byte positions, so the
 * decoder runs in streaming mode and lines are rebuilt across chunks. The stream ends at a
 * `[DONE]` data line; an event cut off before its blank line is dropped, as the SSE spec says.
 */
export async function* parseSSE(body: ReadableStream<Uint8Array>): AsyncGenerator<SSEMessage> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let event = "message";
  let data: string[] = [];

  const flush = (): SSEMessage | null => {
    if (data.length === 0) {
      event = "message";
      return null;
    }
    const msg = { event, data: data.join("\n") };
    event = "message";
    data = [];
    return msg;
  };

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) {
        return;
      }
      buffer += decoder.decode(value, { stream: true });

      let nl: number;
      while ((nl = buffer.search(/\r\n|\n|\r/)) !== -1) {
        const sep = buffer.startsWith("\r\n", nl) ? 2 : 1;
        const line = buffer.slice(0, nl);
        buffer = buffer.slice(nl + sep);

        if (line === "") {
          const msg = flush();
          if (!msg) {
            continue;
          }
          if (msg.data === "[DONE]") {
            return;
          }
          yield msg;
        } else if (line.startsWith(":")) {
          // comment / keep-alive
        } else {
          const colon = line.indexOf(":");
          const field = colon === -1 ? line : line.slice(0, colon);
          let val = colon === -1 ? "" : line.slice(colon + 1);
          if (val.startsWith(" ")) {
            val = val.slice(1);
          }
          if (field === "event") {
            event = val;
          } else if (field === "data") {
            data.push(val);
          }
        }
      }
    }
  } finally {
    reader.cancel().catch(() => {});
  }
}
