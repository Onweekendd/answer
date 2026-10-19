import { AssistantRuntimeProvider } from "@assistant-ui/react";
import { useCallback, useState } from "react";
import { useSearchParams } from "react-router-dom";

import { AgentProcess, ProcessSteps, ProcessThinking } from "@/components/AgentProcess";
import { AgentStep } from "@/components/AgentStep";
import { Thread, type ThreadComponents } from "@/components/assistant-ui/thread.aui";
import { ChatHistory } from "@/components/ChatHistory";
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
 * The chat fills the space between the top bar (3.5rem) and, on phones, the bottom tab bar
 * (about 4rem); the negative margin lets the thread use the full width of the screen.
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
    <div className="-mx-4 -mb-24 md:-mb-8 flex h-[calc(100dvh-3.5rem-4rem)] flex-col md:h-[calc(100dvh-3.5rem)]">
      <ChatHistory current={params.get("c")} onOpen={open} />
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
        <ProductCardsUI />
        <Thread components={components} />
      </TooltipProvider>
    </AssistantRuntimeProvider>
  );
}
