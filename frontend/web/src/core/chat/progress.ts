/** The slice of a message part we read; assistant-ui's part state satisfies it structurally. */
export interface PartLike {
  type: string;
  args?: unknown;
  result?: unknown;
}

const summaryOf = (v: unknown): string =>
  typeof v === "object" && v !== null && typeof (v as { summary?: unknown }).summary === "string"
    ? (v as { summary: string }).summary
    : "";

/**
 * The one line on the folded process box. While the agent works it is the step under way, so a
 * glance says what is happening; once done it is how much was done, since the answer below is
 * what matters then.
 */
export function processHeadline(parts: readonly PartLike[], indices: readonly number[], running: boolean): string {
  const inGroup = indices.map((i) => parts[i]).filter((p): p is PartLike => p !== undefined);
  const steps = inGroup.filter((p) => p.type === "tool-call");

  if (running) {
    const last = inGroup.at(-1);
    if (last?.type === "tool-call") {
      return summaryOf(last.args) || "正在操作浏览器";
    }
    return "思考中";
  }

  if (steps.length === 0) {
    return "思考过程";
  }
  const failed = steps.filter((p) => (p.result as { ok?: unknown } | undefined)?.ok === false).length;
  return failed > 0 ? `执行了 ${steps.length} 步（${failed} 步没做成）` : `执行了 ${steps.length} 步`;
}
