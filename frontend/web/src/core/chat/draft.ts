/**
 * The post draft the agent hands over when the user asks it to post to the community (the
 * `tool_suspend` event). The agent stops there; the user edits the draft, publishes it through
 * the usual post endpoint, or drops it. Mirrors DraftInput in internal/service/purchase_ai/drafts.go.
 */
export interface PostDraft {
  draftId: string;
  title: string;
  content: string;
  /** the pictures the agent picked, the cover first */
  images: string[];
  /** every picture of the chat, so the user can pick others */
  candidates: string[];
  price: number;
  link?: string;
  category?: string;
  /** the AI's own vote, cast first once the post is out: 1 buy, 2 wait, 3 skip */
  vote: 1 | 2 | 3;
}

/** The name of the tool-call part that holds a draft waiting for the user. */
export const PUBLISH_TOOL = "publish_post";

/** What the user did with the draft; it becomes the tool call's result and is saved with the chat. */
export type DraftResult = { status: "published"; questionId: string; title: string } | { status: "dropped" };

export const VOTE_LABEL: Record<PostDraft["vote"], string> = { 1: "买", 2: "再等等", 3: "不买" };

/** The post endpoint takes at most this many pictures and a title of at least this many characters. */
export const MAX_IMAGES = 5;
export const MIN_TITLE = 6;
export const MAX_TITLE = 150;

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const isHttpURL = (v: unknown): v is string => typeof v === "string" && /^https?:\/\//.test(v);
const isURLList = (v: unknown): v is string[] => Array.isArray(v) && v.every(isHttpURL);

/** A draft from the server, checked: the urls end up in src and href attributes. */
export function isPostDraft(v: unknown): v is PostDraft {
  return (
    isRecord(v) &&
    typeof v.draftId === "string" &&
    typeof v.title === "string" &&
    typeof v.content === "string" &&
    isURLList(v.images) &&
    isURLList(v.candidates) &&
    typeof v.price === "number" &&
    (v.link === undefined || isHttpURL(v.link)) &&
    (v.category === undefined || typeof v.category === "string") &&
    (v.vote === 1 || v.vote === 2 || v.vote === 3)
  );
}

export function isDraftResult(v: unknown): v is DraftResult {
  return (
    isRecord(v) &&
    ((v.status === "published" && typeof v.questionId === "string" && typeof v.title === "string") ||
      v.status === "dropped")
  );
}

/** The form the user edits; the price is text while typing. */
export interface DraftForm {
  title: string;
  content: string;
  price: string;
  link: string;
  category: string;
  images: string[];
}

export function formFromDraft(d: PostDraft): DraftForm {
  return {
    title: d.title,
    content: d.content,
    price: d.price > 0 ? String(d.price) : "",
    link: d.link ?? "",
    category: d.category ?? "",
    images: d.images.slice(0, MAX_IMAGES)
  };
}

/** Picks or unpicks a picture, keeping the order they were picked in (the first is the cover). */
export function toggleImage(images: readonly string[], url: string): string[] {
  if (images.includes(url)) {
    return images.filter((u) => u !== url);
  }
  return images.length >= MAX_IMAGES ? [...images] : [...images, url];
}

/** What is wrong with the form, in words for the user; null when it can be published. */
export function checkDraftForm(f: DraftForm): string | null {
  const title = f.title.trim();
  if ([...title].length < MIN_TITLE || [...title].length > MAX_TITLE) {
    return `标题要 ${MIN_TITLE} 到 ${MAX_TITLE} 个字`;
  }
  if (f.images.length === 0) {
    return "至少选一张图";
  }
  const price = f.price.trim();
  if (price !== "" && !(Number(price) >= 0)) {
    return "价格要是一个数字";
  }
  if (f.link.trim() !== "" && !isHttpURL(f.link.trim())) {
    return "链接要以 http:// 或 https:// 开头";
  }
  return null;
}

/** The body of POST /question for the form. Answer needs a tag; the category is it. */
export function toQuestionAdd(f: DraftForm) {
  const category = f.category.trim();
  return {
    title: f.title.trim(),
    content: f.content.trim(),
    tags: category ? [{ slug_name: category, display_name: category, original_text: "" }] : [],
    purchase: {
      images: f.images,
      price: f.price.trim() === "" ? 0 : Number(f.price),
      currency: "CNY",
      link: f.link.trim()
    }
  };
}
