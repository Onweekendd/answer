/**
 * The events the chat endpoint streams, one JSON object per SSE `data:` line. The Go side
 * must emit exactly these; they are the contract between the two.
 */
import { isPostDraft, type PostDraft, PUBLISH_TOOL } from "./draft";

export type ChatEvent =
  | { type: "text"; delta: string }
  /** The model's thinking for one step, sent whole once the step's reply is in. */
  | { type: "reasoning"; delta: string }
  /** For the agent's tools, `args` is `{ summary }` and `result` is `{ summary, ok }`: what the user may see. */
  | { type: "tool_start"; id: string; name: string; args: Record<string, unknown> }
  | { type: "tool_end"; id: string; result: unknown }
  /** A product the agent picked; a card the user can open. Sent again with the same id when it changes. */
  | { type: "product"; product: ProductCard }
  /** A web page the agent made and handed over. Sent again with the same id when it delivers the page again. */
  | { type: "artifact"; artifact: Artifact }
  /** A call the agent stopped at for the user to decide: the post draft, in `input`. */
  | { type: "tool_suspend"; id: string; name: string; input: PostDraft }
  /** What the user may want to say next, at most three; shown as buttons that send it. */
  | { type: "followups"; followups: string[] }
  | { type: "error"; message: string }
  /**
   * The ids the server logged this turn under: the next message follows `leaf`; regenerating this
   * turn's answer reruns `user`, which is empty when the turn was itself a regeneration.
   */
  | { type: "saved"; saved: SavedIds }
  | { type: "done" };

export interface SavedIds {
  user: string;
  leaf: string;
}

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

/**
 * A web page the agent made in its sandbox and handed over (artifacts.go). The page and its
 * screenshot are stored on our own server; the page is served with a sandbox CSP, and it is shown
 * in a sandboxed iframe, since the model wrote it from what it read on other sites.
 */
export interface Artifact {
  id: number;
  title: string;
  url: string;
  /** The page shot when it was delivered: the card's picture, and what the user saves to share. */
  image?: string;
  size: number;
}

/** The name of the one data part that holds all the cards of a message. */
export const PRODUCTS_PART = "products";

/** The name of the one data part that holds all the artifacts of a message. */
export const ARTIFACTS_PART = "artifacts";

/** The name of the data part that holds the answer's follow-up suggestions; nothing renders it in the message. */
export const FOLLOWUPS_PART = "followups";

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
  | { type: "data"; name: typeof PRODUCTS_PART; data: ProductCard[] }
  | { type: "data"; name: typeof ARTIFACTS_PART; data: Artifact[] }
  | { type: "data"; name: typeof FOLLOWUPS_PART; data: string[] };

/** The server reported a failure mid-stream. */
export class ChatStreamError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ChatStreamError";
  }
}

/** Folds one event into the message so far; never mutates `parts`. */
export function applyEvent(parts: readonly ContentPart[], event: ChatEvent): ContentPart[] {
  return orderParts(fold(parts, event));
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
      const old = parts.find(isProducts);
      const cards = [...(old?.data ?? []).filter((c) => c.id !== event.product.id), event.product];
      cards.sort((a, b) => a.id - b.id);
      return [...parts.filter((p) => p !== old), { type: "data", name: PRODUCTS_PART, data: cards }];
    }
    case "artifact": {
      // like the products: one part, and a page delivered again replaces the old one
      const old = parts.find(isArtifacts);
      const pages = [...(old?.data ?? []).filter((a) => a.id !== event.artifact.id), event.artifact];
      pages.sort((a, b) => a.id - b.id);
      return [...parts.filter((p) => p !== old), { type: "data", name: ARTIFACTS_PART, data: pages }];
    }
    case "tool_suspend":
      // a part of its own, not the step: the step stays in the process box, the draft goes below the answer
      return [
        ...parts,
        {
          type: "tool-call",
          toolCallId: `${event.id}:review`,
          toolName: PUBLISH_TOOL,
          args: { ...event.input },
          argsText: JSON.stringify(event.input)
        }
      ];
    case "followups":
      return [
        ...parts.filter((p) => !(p.type === "data" && p.name === FOLLOWUPS_PART)),
        { type: "data", name: FOLLOWUPS_PART, data: event.followups }
      ];
    case "error":
      throw new ChatStreamError(event.message);
    case "saved":
    case "done":
      return [...parts];
  }
}

/**
 * A message reads top to bottom: the process box and the answer, then the pages the agent made
 * (what the user asked for), the products the answer talked about, a draft waiting for the user,
 * then what to ask next. Cards arrive mid-run,
 * before the answer, so every fold puts the parts back in this order; the sort is stable, so the
 * steps and the answer keep theirs. Stored messages from before this order get it on load.
 */
export function orderParts<P extends { type: string }>(parts: readonly P[]): P[] {
  const rank = (p: P) => {
    const part = p as unknown as ContentPart;
    if (isArtifacts(part)) {
      return 1;
    }
    if (isProducts(part)) {
      return 2;
    }
    if (part.type === "tool-call" && part.toolName === PUBLISH_TOOL) {
      return 3;
    }
    if (part.type === "data" && part.name === FOLLOWUPS_PART) {
      return 4;
    }
    return 0;
  };
  return [...parts].sort((a, b) => rank(a) - rank(b));
}

function isProducts(p: ContentPart): p is Extract<ContentPart, { name: typeof PRODUCTS_PART }> {
  return p.type === "data" && p.name === PRODUCTS_PART;
}

function isArtifacts(p: ContentPart): p is Extract<ContentPart, { name: typeof ARTIFACTS_PART }> {
  return p.type === "data" && p.name === ARTIFACTS_PART;
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

const isHTTP = (v: unknown): v is string => typeof v === "string" && /^https?:\/\//.test(v);

/**
 * A page needs an id, a title and a page. The page goes into an iframe src and the picture into an
 * img src, so both must be http(s): a javascript: or data: page would run in our page's origin.
 */
function isArtifact(v: unknown): v is Artifact {
  return (
    isRecord(v) &&
    typeof v.id === "number" &&
    typeof v.title === "string" &&
    isHTTP(v.url) &&
    (v.image === undefined || isHTTP(v.image)) &&
    typeof v.size === "number"
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
  return toEvent(raw);
}

/** Checks one decoded event (from the stream, or from a logged turn being replayed); null if malformed. */
export function toEvent(raw: unknown): ChatEvent | null {
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
    case "artifact":
      return isArtifact(raw.artifact) ? { type: "artifact", artifact: raw.artifact } : null;
    case "tool_suspend":
      return typeof raw.id === "string" && typeof raw.name === "string" && isPostDraft(raw.input)
        ? { type: "tool_suspend", id: raw.id, name: raw.name, input: raw.input }
        : null;
    case "followups": {
      const followups = Array.isArray(raw.followups)
        ? raw.followups.filter((f): f is string => typeof f === "string" && f.trim() !== "").slice(0, 3)
        : [];
      return followups.length > 0 ? { type: "followups", followups } : null;
    }
    case "error":
      return { type: "error", message: typeof raw.message === "string" ? raw.message : "出错了" };
    case "saved":
      return isRecord(raw.saved) && typeof raw.saved.leaf === "string" && raw.saved.leaf !== ""
        ? {
            type: "saved",
            saved: { user: typeof raw.saved.user === "string" ? raw.saved.user : "", leaf: raw.saved.leaf }
          }
        : null;
    case "done":
      return { type: "done" };
    default:
      return null;
  }
}
