/**
 * The events the chat endpoint streams, one JSON object per SSE `data:` line. The Go side
 * must emit exactly these; they are the contract between the two.
 */
export type ChatEvent =
  | { type: "text"; delta: string }
  /** The model's thinking for one step, sent whole once the step's reply is in. */
  | { type: "reasoning"; delta: string }
  /** For the agent's tools, `args` is `{ summary }` and `result` is `{ summary, ok }`: what the user may see. */
  | { type: "tool_start"; id: string; name: string; args: Record<string, unknown> }
  | { type: "tool_end"; id: string; result: unknown }
  /** A product the agent picked; a card the user can open. Sent again with the same id when it changes. */
  | { type: "product"; product: ProductCard }
  | { type: "error"; message: string }
  | { type: "done" };

/**
 * A product card, as internal/service/purchase_ai/products.go sends it. The link and the image
 * come from the page itself, not from the model; the image is stored on our own server.
 */
export interface ProductCard {
  id: number;
  title: string;
  shop?: string;
  spec?: string;
  price?: string;
  priceNote?: string;
  note?: string;
  url: string;
  image?: string;
}

/** The name of the one data part that holds all the cards of a message. */
export const PRODUCTS_PART = "products";

/** The shapes of assistant-ui's message content parts that we produce. */
export type ContentPart =
  | { type: "text"; text: string }
  | { type: "reasoning"; text: string }
  | {
      type: "tool-call";
      toolCallId: string;
      toolName: string;
      args: Record<string, unknown>;
      argsText: string;
      result?: unknown;
    }
  | { type: "data"; name: typeof PRODUCTS_PART; data: ProductCard[] };

/** The server reported a failure mid-stream. */
export class ChatStreamError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ChatStreamError";
  }
}

/** Folds one event into the message so far; never mutates `parts`. */
export function applyEvent(parts: readonly ContentPart[], event: ChatEvent): ContentPart[] {
  return placeProducts(fold(parts, event));
}

function fold(parts: readonly ContentPart[], event: ChatEvent): ContentPart[] {
  switch (event.type) {
    case "text":
    case "reasoning": {
      // a delta joins the part before it only if that part is the same kind: thinking from the
      // next step, after a tool call, starts a part of its own
      const last = parts.at(-1);
      if (last?.type === event.type) {
        return [...parts.slice(0, -1), { type: event.type, text: last.text + event.delta }];
      }
      return [...parts, { type: event.type, text: event.delta }];
    }
    case "tool_start":
      return [
        ...parts,
        {
          type: "tool-call",
          toolCallId: event.id,
          toolName: event.name,
          args: event.args,
          argsText: JSON.stringify(event.args)
        }
      ];
    case "tool_end":
      return parts.map((p) =>
        p.type === "tool-call" && p.toolCallId === event.id ? { ...p, result: event.result } : p
      );
    case "product": {
      // all the cards live in one part; a card sent again with the same id replaces the old one
      const old = parts.find((p) => p.type === "data");
      const cards = [...(old?.data ?? []).filter((c) => c.id !== event.product.id), event.product];
      cards.sort((a, b) => a.id - b.id);
      return [...parts.filter((p) => p !== old), { type: "data", name: PRODUCTS_PART, data: cards }];
    }
    case "error":
      throw new ChatStreamError(event.message);
    case "done":
      return [...parts];
  }
}

/**
 * Keeps the products part right after the last thinking or step, so it sits between the process
 * box and the answer. Steps that come after a card must stay next to the steps before it, or the
 * folded process box would break in two around the cards.
 */
function placeProducts(parts: ContentPart[]): ContentPart[] {
  const products = parts.find((p) => p.type === "data");
  if (!products) {
    return parts;
  }
  const rest = parts.filter((p) => p !== products);
  const at = rest.findLastIndex((p) => p.type === "reasoning" || p.type === "tool-call") + 1;
  return [...rest.slice(0, at), products, ...rest.slice(at)];
}

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

/**
 * A card needs an id, a title and a link to be worth showing. The link must be http(s): it goes
 * into an href, and a javascript: link there would run in our page.
 */
function isProductCard(v: unknown): v is ProductCard {
  return (
    isRecord(v) &&
    typeof v.id === "number" &&
    typeof v.title === "string" &&
    typeof v.url === "string" &&
    /^https?:\/\//.test(v.url) &&
    (v.image === undefined || (typeof v.image === "string" && /^https?:\/\//.test(v.image)))
  );
}

/** Parses one SSE data payload; null for anything malformed so one bad line cannot kill the stream. */
export function parseEvent(data: string): ChatEvent | null {
  let raw: unknown;
  try {
    raw = JSON.parse(data);
  } catch {
    return null;
  }
  if (!isRecord(raw)) {
    return null;
  }

  switch (raw.type) {
    case "text":
    case "reasoning":
      return typeof raw.delta === "string" ? { type: raw.type, delta: raw.delta } : null;
    case "tool_start":
      return typeof raw.id === "string" && typeof raw.name === "string"
        ? { type: "tool_start", id: raw.id, name: raw.name, args: isRecord(raw.args) ? raw.args : {} }
        : null;
    case "tool_end":
      return typeof raw.id === "string" ? { type: "tool_end", id: raw.id, result: raw.result } : null;
    case "product":
      return isProductCard(raw.product) ? { type: "product", product: raw.product } : null;
    case "error":
      return { type: "error", message: typeof raw.message === "string" ? raw.message : "出错了" };
    case "done":
      return { type: "done" };
    default:
      return null;
  }
}
