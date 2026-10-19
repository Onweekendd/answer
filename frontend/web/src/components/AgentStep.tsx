import type { ToolCallMessagePartComponent } from "@assistant-ui/react";
import { CheckIcon, LoaderIcon, XIcon } from "lucide-react";

import { ToolFallback } from "@/components/assistant-ui/tool-fallback.aui";

/** What the server sends for each agent step: a sentence for the user, never the page itself. */
interface StepView {
  summary?: unknown;
  ok?: unknown;
}

/**
 * One line per browser step: what the agent is doing, then how it went. A tool call without a
 * summary (the mock's) falls back to the stock card that shows the raw arguments.
 */
export const AgentStep: ToolCallMessagePartComponent = (props) => {
  const args = props.args as StepView;
  const result = props.result as StepView | undefined;
  if (typeof args.summary !== "string") {
    return <ToolFallback {...props} />;
  }

  const failed = result?.ok === false || props.status.type === "incomplete";
  const running = result === undefined && !failed;
  const outcome = typeof result?.summary === "string" ? result.summary : "";

  return (
    <div className="flex items-start gap-2 py-1 text-sm">
      <span className="mt-0.5 shrink-0 text-muted-foreground" aria-hidden>
        {running ? (
          <LoaderIcon className="size-4 animate-spin" />
        ) : failed ? (
          <XIcon className="size-4 text-destructive" />
        ) : (
          <CheckIcon className="size-4" />
        )}
      </span>
      <span className="min-w-0 wrap-break-word">
        {args.summary}
        {outcome && <span className="text-muted-foreground"> · {outcome}</span>}
      </span>
    </div>
  );
};
