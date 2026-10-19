// @vitest-environment jsdom
import { describe, expect, it } from "vitest";

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { ZoomableImage } from "./ZoomableImage";

describe("ZoomableImage", () => {
  it("opens the image over the page instead of following a link", async () => {
    // Arrange
    render(<ZoomableImage src="/uploads/agent/a.jpg" alt="图1" />);

    // Act
    await userEvent.click(screen.getByRole("button", { name: "查看大图：图1" }));

    // Assert
    expect(screen.queryByRole("link")).toBeNull();
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.getAllByAltText("图1")).toHaveLength(2);
  });
});
