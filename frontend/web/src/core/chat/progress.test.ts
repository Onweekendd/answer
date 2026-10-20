import { describe, expect, it } from "vitest";

import { processHeadline } from "./progress";

const step = (summary: string, ok?: boolean) => ({
  type: "tool-call",
  args: { summary },
  result: ok === undefined ? undefined : { summary: "", ok }
});
const thinking = { type: "reasoning" };

describe("processHeadline", () => {
  it("shows the step under way while the agent works", () => {
    const parts = [thinking, step("打开 www.goofish.com", true), thinking, step("点击「iPhone 14 128G」")];

    expect(processHeadline(parts, [0, 1, 2, 3], true)).toBe("点击「iPhone 14 128G」");
  });

  it("says it is thinking when the newest part is thinking", () => {
    expect(processHeadline([step("查看页面", true), thinking], [0, 1], true)).toBe("思考中");
  });

  it("counts the steps once done, and the ones that failed", () => {
    const parts = [thinking, step("a", true), step("b", false), thinking, step("c", true), { type: "text" }];

    expect(processHeadline(parts, [0, 1, 2, 3, 4], false)).toBe("执行了 3 步（1 步没做成）");
    expect(processHeadline([step("a", true)], [0], false)).toBe("执行了 1 步");
  });

  it("only reads the parts in the group", () => {
    const parts = [step("outside", true), thinking, step("inside", true)];

    expect(processHeadline(parts, [1, 2], false)).toBe("执行了 1 步");
  });

  it("names a box with thinking only, as when there is no browser", () => {
    expect(processHeadline([thinking], [0], false)).toBe("思考过程");
  });
});
