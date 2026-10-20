import { makeAssistantDataUI, ThreadPrimitive, useAuiState } from "@assistant-ui/react";
import { CornerDownRightIcon } from "lucide-react";

import { FOLLOWUPS_PART } from "@/core/chat/mapper";

/**
 * What the user may want to say next, at the bottom of the answer, one per line. Only under the
 * last answer once it is done: further up the chat has moved on, and a tap there would not fit.
 */
export function Followups({ followups }: { followups: readonly string[] }) {
  const show = useAuiState((s) => s.message.isLast && !s.thread.isRunning);
  if (!show || followups.length === 0) {
    return null;
  }
  return (
    <div role="group" aria-label="接着问" className="mt-4 flex flex-col items-start gap-2">
      {followups.map((text) => (
        <ThreadPrimitive.Suggestion
          key={text}
          prompt={text}
          send
          className="group flex max-w-full items-center gap-2 rounded-xl bg-muted px-3.5 py-2 text-left text-sm text-foreground/90 transition-colors hover:bg-accent hover:text-accent-foreground"
        >
          <CornerDownRightIcon className="size-3.5 shrink-0 text-muted-foreground group-hover:text-accent-foreground" />
          <span className="min-w-0">{text}</span>
        </ThreadPrimitive.Suggestion>
      ))}
    </div>
  );
}

/** Renders the answer's follow-ups part (core/chat/mapper.ts) inside the message. */
export const FollowupsUI = makeAssistantDataUI<string[]>({
  name: FOLLOWUPS_PART,
  render: ({ data }) => <Followups followups={Array.isArray(data) ? data : []} />
});
