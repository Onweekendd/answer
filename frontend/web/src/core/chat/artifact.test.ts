import { describe, expect, it } from "vitest";

import { formatSize } from "./artifact";

describe("formatSize", () => {
  it("says a page's size the way a file manager would", () => {
    expect(formatSize(0)).toBe("0 B");
    expect(formatSize(900)).toBe("900 B");
    expect(formatSize(48_213)).toBe("47 KB");
    expect(formatSize(1_536)).toBe("1.5 KB");
    expect(formatSize(3_400_000)).toBe("3.2 MB");
  });
});
