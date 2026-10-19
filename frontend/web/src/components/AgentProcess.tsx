import { useAuiState } from "@assistant-ui/react";
import { CheckIcon, ChevronRightIcon, LoaderIcon } from "lucide-react";
import type { PropsWithChildren } from "react";

import type { ThreadGroupPart } from "@/components/assistant-ui/thread.aui";
import { type PartLike, processHeadline } from "@/core/chat/progress";

/**
 * All of the agent's thinking and steps before the answer, folded into one box. Closed, it is a
 * single line: the step under way, or how many steps were taken. Open, the thinking and the
 * steps alternate in the order they happened.
 */
export function AgentProcess({ group, children }: PropsWithChildren<{ group: ThreadGroupPart }>) {
  const running = group.status.type === "running";
  // a string, not the parts: the box re-renders only when its line changes
  const headline = useAuiState((s) => processHeadline(s.message.parts as readonly PartLike[], group.indices, running));

  return (
    <details className="group/process my-2 rounded-lg border border-border/60 text-sm">
      <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-muted-foreground select-none [&::-webkit-details-marker]:hidden">
        {running ? <LoaderIcon className="size-4 shrink-0 animate-spin" /> : <CheckIcon className="size-4 shrink-0" />}
        <span className="min-w-0 flex-1 truncate">{headline}</span>
        <ChevronRightIcon className="size-4 shrink-0 transition-transform group-open/process:rotate-90" />
      </summary>
      <div className="flex flex-col gap-1 border-t border-border/60 px-3 py-2">{children}</div>
    </details>
  );
}

/** Inside the box the thinking needs no box of its own: a few muted lines between the steps. */
export function ProcessThinking({ children }: PropsWithChildren) {
  return <div className="text-xs leading-relaxed text-muted-foreground">{children}</div>;
}

/** Likewise the steps: one line each, no "N tool calls" header. */
export function ProcessSteps({ children }: PropsWithChildren) {
  return <div>{children}</div>;
}
