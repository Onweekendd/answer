// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { PostDraft } from "@/core/chat/draft";

const publishPost = vi.hoisted(() => vi.fn());
const draftVote = vi.hoisted(() => vi.fn());
const draftResult = vi.hoisted(() => vi.fn());
vi.mock("@/lib/session", () => ({ api: { publishPost, draftVote, draftResult }, tokenStore: {} }));

import { PostDraftCard } from "./PostDraftCard";

const here = `${window.location.origin}/uploads`;
const draft: PostDraft = {
  draftId: "d1",
  title: "iPad Air M3 现在买还是等双 11？",
  content: "预算 4000",
  images: [`${here}/agent/a.jpg`],
  candidates: [`${here}/post/u.jpg`, `${here}/agent/a.jpg`],
  price: 4299,
  category: "数码",
  vote: 2
};

function renderCard(onDone = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <PostDraftCard draft={draft} result={undefined} onDone={onDone} />
    </QueryClientProvider>
  );
  return onDone;
}

afterEach(cleanup);

beforeEach(() => {
  publishPost.mockReset();
  draftVote.mockReset();
  draftResult.mockReset();
  draftResult.mockResolvedValue(null);
});

describe("PostDraftCard", () => {
  it("publishes the edited draft, then lets the AI vote, and reports the post", async () => {
    // Arrange
    publishPost.mockResolvedValue({ id: "42" });
    draftVote.mockResolvedValue(null);
    const onDone = renderCard();
    const title = screen.getByLabelText("标题");

    // Act: change the title and add the user's screenshot as a second picture
    await userEvent.clear(title);
    await userEvent.type(title, "iPad Air M3 双 11 前该不该买");
    await userEvent.click(screen.getByRole("button", { name: "选这张图" }));
    await userEvent.click(screen.getByRole("button", { name: "发布到社区" }));

    // Assert
    expect(publishPost).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "iPad Air M3 双 11 前该不该买",
        purchase: expect.objectContaining({ images: [`${here}/agent/a.jpg`, `${here}/post/u.jpg`], price: 4299 })
      })
    );
    expect(draftVote).toHaveBeenCalledWith("d1", "42");
    const published = { status: "published", questionId: "42", title: "iPad Air M3 双 11 前该不该买" };
    expect(draftResult).toHaveBeenCalledWith("d1", published); // the chat remembers it
    expect(onDone).toHaveBeenCalledWith(published);
  });

  it("still counts as published when the chat cannot remember it", async () => {
    // Arrange
    publishPost.mockResolvedValue({ id: "42" });
    draftVote.mockResolvedValue(null);
    draftResult.mockRejectedValue(new Error("offline"));
    const onDone = renderCard();

    // Act
    await userEvent.click(screen.getByRole("button", { name: "发布到社区" }));

    // Assert: the post is out, whatever happens to the note about it
    expect(onDone).toHaveBeenCalledWith(expect.objectContaining({ status: "published" }));
  });

  it("still counts as published when only the AI's vote fails", async () => {
    // Arrange
    publishPost.mockResolvedValue({ id: "42" });
    draftVote.mockRejectedValue(new Error("expired"));
    const onDone = renderCard();

    // Act
    await userEvent.click(screen.getByRole("button", { name: "发布到社区" }));

    // Assert
    expect(onDone).toHaveBeenCalledWith(expect.objectContaining({ status: "published" }));
  });

  it("shows what is wrong and does not post a bad form", async () => {
    // Arrange
    const onDone = renderCard();

    // Act
    await userEvent.clear(screen.getByLabelText("标题"));
    await userEvent.type(screen.getByLabelText("标题"), "买吗");
    await userEvent.click(screen.getByRole("button", { name: "发布到社区" }));

    // Assert
    expect(await screen.findByRole("alert")).toHaveTextContent("标题要 6 到 150 个字");
    expect(publishPost).not.toHaveBeenCalled();
    expect(onDone).not.toHaveBeenCalled();
  });

  it("drops the draft without posting", async () => {
    // Arrange
    const onDone = renderCard();

    // Act
    await userEvent.click(screen.getByRole("button", { name: "放弃" }));

    // Assert
    expect(onDone).toHaveBeenCalledWith({ status: "dropped" });
    expect(draftResult).toHaveBeenCalledWith("d1", { status: "dropped" });
    expect(publishPost).not.toHaveBeenCalled();
  });

  it("shows a published draft as a link to the post", () => {
    // Act
    render(
      <PostDraftCard draft={draft} result={{ status: "published", questionId: "42", title: "t" }} onDone={vi.fn()} />
    );

    // Assert
    expect(screen.getByRole("link", { name: "去看看" })).toHaveAttribute("href", "/questions/42");
  });
});
