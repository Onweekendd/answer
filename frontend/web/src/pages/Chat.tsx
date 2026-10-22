import { AssistantRuntimeProvider } from "@assistant-ui/react";
import { useCallback, useState } from "react";
import { useSearchParams } from "react-router-dom";

import { AgentProcess, ProcessSteps, ProcessThinking } from "@/components/AgentProcess";
import { AgentStep } from "@/components/AgentStep";
import { ArtifactCardsUI } from "@/components/ArtifactCards";
import { Thread, type ThreadComponents } from "@/components/assistant-ui/thread.aui";
import { ChatTopBar } from "@/components/ChatHistory";
import { FollowupsUI } from "@/components/Followups";
import { PostDraftUI } from "@/components/PostDraftCard";
import { ProductCardsUI } from "@/components/ProductCards";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useChatRuntime } from "@/hooks/useChatRuntime";

/** Module level, so the thread does not see new components on every render. */
const components: ThreadComponents = {
  ChainOfThought: AgentProcess,
  ReasoningGroup: ProcessThinking,
  ToolGroup: ProcessSteps,
  ToolFallback: AgentStep
};

let newChats = 0;

/**
 * The chat fills the screen: its own top bar, then the thread (AppShell leaves the chat route
 * without the site header and tab bar).
 *
 * ?c=<id> names the saved conversation. A new conversation gets its id on its first message;
 * the address then changes but the session keeps its key, so the answer being written goes on.
 */
export default function Chat() {
  const [params, setParams] = useSearchParams();
  const [session, setSession] = useState(() => {
    const id = params.get("c");
    return { key: id ?? `new-${++newChats}`, id };
  });

  const open = (id: string | null) => {
    setSession({ key: id ?? `new-${++newChats}`, id });
    setParams(id ? { c: id } : {});
  };
  const onCreated = useCallback((id: string) => setParams({ c: id }, { replace: true }), [setParams]);

  return (
    <div className="flex h-full flex-col">
      <ChatTopBar current={params.get("c")} onOpen={open} />
      <div className="min-h-0 flex-1">
        <ChatSession key={session.key} chatId={session.id} onCreated={onCreated} />
      </div>
    </div>
  );
}

function ChatSession({ chatId, onCreated }: { chatId: string | null; onCreated: (id: string) => void }) {
  const runtime = useChatRuntime(chatId, onCreated);

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <TooltipProvider>
        <ArtifactCardsUI />
        <ProductCardsUI />
        <PostDraftUI />
        <FollowupsUI />
        <Thread components={components} />
      </TooltipProvider>
    </AssistantRuntimeProvider>
  );
}
