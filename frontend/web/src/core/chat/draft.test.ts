import { describe, expect, it } from "vitest";

import { toChatMessages } from "./api";
import {
  checkDraftForm,
  type DraftForm,
  formFromDraft,
  isPostDraft,
  type PostDraft,
  toggleImage,
  toQuestionAdd
} from "./draft";
import { applyEvent, type ContentPart, parseEvent } from "./mapper";

const draft: PostDraft = {
  draftId: "d1",
  title: "iPad Air M3 现在买还是等双 11？",
  content: "预算 4000",
  images: ["http://site/uploads/agent/a.jpg"],
  candidates: ["http://site/uploads/post/u.jpg", "http://site/uploads/agent/a.jpg"],
  price: 4299,
  link: "https://item.jd.com/100.html",
  category: "数码",
  vote: 2
};

const form = (over: Partial<DraftForm> = {}): DraftForm => ({ ...formFromDraft(draft), ...over });

describe("isPostDraft", () => {
  it("takes a well formed draft", () => {
    expect(isPostDraft(draft)).toBe(true);
  });

  it("refuses a draft whose pictures are not http(s): they go into src", () => {
    expect(isPostDraft({ ...draft, images: ["javascript:alert(1)"] })).toBe(false);
    expect(isPostDraft({ ...draft, vote: 4 })).toBe(false);
  });
});

describe("toggleImage", () => {
  it("adds at the end, removes in place, and stops at five", () => {
    // Arrange
    const five = ["1", "2", "3", "4", "5"];

    // Act + Assert
    expect(toggleImage(["a"], "b")).toEqual(["a", "b"]);
    expect(toggleImage(["a", "b", "c"], "b")).toEqual(["a", "c"]);
    expect(toggleImage(five, "6")).toEqual(five);
  });
});

describe("checkDraftForm", () => {
  it.each([
    [{ title: "买吗" }, "标题要 6 到 150 个字"],
    [{ images: [] }, "至少选一张图"],
    [{ price: "四千" }, "价格要是一个数字"],
    [{ link: "item.jd.com/1" }, "链接要以 http:// 或 https:// 开头"]
  ])("says what is wrong: %o", (over, message) => {
    expect(checkDraftForm(form(over))).toBe(message);
  });

  it("passes a good form, with or without a price", () => {
    expect(checkDraftForm(form())).toBeNull();
    expect(checkDraftForm(form({ price: "" }))).toBeNull();
  });
});

describe("toQuestionAdd", () => {
  it("turns the form into the post endpoint's body, the category as the tag", () => {
    expect(toQuestionAdd(form({ title: " 标题标题标题 ", price: "4199.5" }))).toEqual({
      title: "标题标题标题",
      content: "预算 4000",
      tags: [{ slug_name: "数码", display_name: "数码", original_text: "" }],
      purchase: {
        images: ["http://site/uploads/agent/a.jpg"],
        price: 4199.5,
        currency: "CNY",
        link: "https://item.jd.com/100.html"
      }
    });
  });
});

describe("the tool_suspend event", () => {
  it("parses only with a well formed draft", () => {
    expect(parseEvent(JSON.stringify({ type: "tool_suspend", id: "3-0", name: "draft_post", input: draft }))).toEqual({
      type: "tool_suspend",
      id: "3-0",
      name: "draft_post",
      input: draft
    });
    expect(parseEvent(JSON.stringify({ type: "tool_suspend", id: "3-0", name: "draft_post", input: {} }))).toBeNull();
  });

  it("becomes a publish_post part that stays below the answer", () => {
    // Arrange
    let parts: ContentPart[] = [];

    // Act: the draft arrives before the answer's text
    parts = applyEvent(parts, { type: "tool_start", id: "3-0", name: "draft_post", args: { summary: "整理帖子草稿" } });
    parts = applyEvent(parts, { type: "tool_suspend", id: "3-0", name: "draft_post", input: draft });
    parts = applyEvent(parts, { type: "tool_end", id: "3-0", result: { summary: "草稿交给你确认", ok: true } });
    parts = applyEvent(parts, { type: "text", delta: "我把这次的讨论整理成了一篇帖子草稿" });

    // Assert
    expect(parts.map((p) => (p.type === "tool-call" ? p.toolName : p.type))).toEqual([
      "draft_post",
      "text",
      "publish_post"
    ]);
    const last = parts.at(-1);
    expect(last?.type === "tool-call" && last.toolCallId).toBe("3-0:review");
  });
});

describe("toChatMessages with a draft", () => {
  it("tells the model what became of the draft", () => {
    // Act
    const got = toChatMessages([
      {
        role: "assistant",
        content: [
          { type: "text", text: "草稿在下面" },
          {
            type: "tool-call",
            toolName: "publish_post",
            result: { status: "published", questionId: "9", title: "iPad 等不等" }
          }
        ]
      }
    ]);

    // Assert
    expect(got[0].text).toBe("草稿在下面\n（用户已经把帖子「iPad 等不等」发到了社区）");
  });
});
