// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { Artifact } from "@/core/chat/mapper";

import { ArtifactCards } from "./ArtifactCards";

const trip: Artifact = {
  id: 1,
  title: "成都 4 天行程",
  url: "https://buy.example.com/uploads/agent-files/a1.html",
  image: "https://buy.example.com/uploads/agent-files/b2.png",
  size: 48_213
};

describe("ArtifactCards", () => {
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("shows a card with the page's picture, title and size; the page itself is not loaded yet", () => {
    // Act
    render(<ArtifactCards artifacts={[trip]} />);

    // Assert
    expect(screen.getByRole("button", { name: /成都 4 天行程/ })).toHaveTextContent("网页 · 47 KB");
    expect(document.querySelector("img")).toHaveAttribute("src", trip.image);
    expect(document.querySelector("iframe")).toBeNull();
  });

  it("opens the page in an isolated frame: scripts run, our origin and login stay out of reach", async () => {
    // Arrange
    render(<ArtifactCards artifacts={[trip]} />);

    // Act
    await userEvent.click(screen.getByRole("button", { name: /成都 4 天行程/ }));

    // Assert
    const frame = await screen.findByTitle("成都 4 天行程");
    expect(frame.tagName).toBe("IFRAME");
    expect(frame).toHaveAttribute("src", `${trip.url}?preview=2`);
    expect(frame).toHaveAttribute("sandbox", "allow-scripts allow-popups allow-popups-to-escape-sandbox");
    expect(frame.getAttribute("sandbox")).not.toContain("allow-same-origin");
    expect(frame).toHaveAttribute("referrerpolicy", "no-referrer");
  });

  it("lets the user save the picture to share it, and open the page on its own", async () => {
    // Arrange
    render(<ArtifactCards artifacts={[trip]} />);

    // Act
    await userEvent.click(screen.getByRole("button", { name: /成都 4 天行程/ }));

    // Assert
    const save = await screen.findByRole("link", { name: "保存为图片" });
    expect(save).toHaveAttribute("href", trip.image);
    expect(save).toHaveAttribute("download");
    const open = screen.getByRole("link", { name: "在新标签页打开" });
    expect(open).toHaveAttribute("href", `${trip.url}?preview=2`);
    expect(open).toHaveAttribute("rel", expect.stringContaining("noopener"));
  });

  it("without a picture there is nothing to save, but the page still opens", async () => {
    // Arrange
    const { image: _, ...noImage } = trip;
    render(<ArtifactCards artifacts={[noImage]} />);

    // Act
    await userEvent.click(screen.getByRole("button", { name: /成都 4 天行程/ }));

    // Assert
    expect(await screen.findByTitle("成都 4 天行程")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "保存为图片" })).toBeNull();
  });

  it("stops a stalled preview, shows its picture and lets the reader retry", async () => {
    const timers = vi.spyOn(window, "setTimeout");
    render(<ArtifactCards artifacts={[trip]} />);
    await userEvent.click(screen.getByRole("button", { name: /成都 4 天行程/ }));
    const deadline = timers.mock.calls.find(([, delay]) => delay === 15_000)?.[0];
    expect(deadline).toBeTypeOf("function");
    act(() => {
      if (typeof deadline === "function") deadline();
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("预览暂时没能加载");
    expect(screen.getByAltText(`${trip.title}的预览图片`)).toHaveAttribute("src", trip.image);
    await userEvent.click(screen.getByRole("button", { name: "重新加载" }));
    expect(screen.getByTitle(trip.title)).toBeInTheDocument();
    fireEvent.load(screen.getByTitle(trip.title));
    expect(screen.queryByRole("status")).toBeNull();
  });
});
