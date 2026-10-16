import { describe, expect, it } from "vitest";

import { parseSSE } from "./sse";

const enc = new TextEncoder();

// Pull-based so an error arrives only after the earlier chunks were read, like a connection
// that drops mid-stream (controller.error() right after enqueue() would discard the queue).
function streamOf(chunks: string[], error?: Error) {
  const queue = [...chunks];
  return new ReadableStream<Uint8Array>({
    pull(controller) {
      const next = queue.shift();
      if (next !== undefined) {
        controller.enqueue(enc.encode(next));
      } else if (error) {
        controller.error(error);
      } else {
        controller.close();
      }
    }
  });
}

async function collect(stream: ReadableStream<Uint8Array>) {
  const out: { event: string; data: string }[] = [];
  for await (const e of parseSSE(stream)) {
    out.push(e);
  }
  return out;
}

describe("parseSSE", () => {
  it('parses a single event with the default name "message"', async () => {
    expect(await collect(streamOf(["data: hello\n\n"]))).toEqual([{ event: "message", data: "hello" }]);
  });

  it("reads the event name", async () => {
    expect(await collect(streamOf(['event: tool\ndata: {"a":1}\n\n']))).toEqual([{ event: "tool", data: '{"a":1}' }]);
  });

  it("handles several events in one chunk", async () => {
    const got = await collect(streamOf(["data: a\n\ndata: b\n\n"]));

    expect(got.map((e) => e.data)).toEqual(["a", "b"]);
  });

  it("reassembles an event split across chunks, even mid-character", async () => {
    const bytes = enc.encode("data: 你好\n\n");
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        // cut inside the 3-byte "你"
        c.enqueue(bytes.slice(0, 8));
        c.enqueue(bytes.slice(8));
        c.close();
      }
    });

    expect(await collect(stream)).toEqual([{ event: "message", data: "你好" }]);
  });

  it("joins multiple data lines with a newline", async () => {
    expect(await collect(streamOf(["data: a\ndata: b\n\n"]))).toEqual([{ event: "message", data: "a\nb" }]);
  });

  it("accepts CRLF line endings", async () => {
    const got = await collect(streamOf(["data: a\r\n\r\ndata: b\r\n\r\n"]));

    expect(got.map((e) => e.data)).toEqual(["a", "b"]);
  });

  it("ignores comment lines (keep-alives)", async () => {
    expect(await collect(streamOf([": ping\n\ndata: a\n\n"]))).toEqual([{ event: "message", data: "a" }]);
  });

  it('accepts "data:" without a space after the colon', async () => {
    expect(await collect(streamOf(["data:a\n\n"]))).toEqual([{ event: "message", data: "a" }]);
  });

  it("stops at [DONE] and yields nothing after it", async () => {
    const got = await collect(streamOf(["data: a\n\ndata: [DONE]\n\ndata: late\n\n"]));

    expect(got.map((e) => e.data)).toEqual(["a"]);
  });

  it("discards a trailing event that never got its blank line", async () => {
    const got = await collect(streamOf(["data: a\n\ndata: cut off"]));

    expect(got.map((e) => e.data)).toEqual(["a"]);
  });

  it("propagates a stream error after delivering the events before it", async () => {
    const got: string[] = [];
    const run = async () => {
      for await (const e of parseSSE(streamOf(["data: a\n\n"], new Error("connection reset")))) {
        got.push(e.data);
      }
    };

    await expect(run()).rejects.toThrow("connection reset");
    expect(got).toEqual(["a"]);
  });
});
