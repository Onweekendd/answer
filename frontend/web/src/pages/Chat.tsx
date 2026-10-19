import { AssistantRuntimeProvider } from "@assistant-ui/react";

import { AgentProcess, ProcessSteps, ProcessThinking } from "@/components/AgentProcess";
import { AgentStep } from "@/components/AgentStep";
import { Thread, type ThreadComponents } from "@/components/assistant-ui/thread.aui";
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

/**
 * The chat fills the space between the top bar (3.5rem) and, on phones, the bottom tab bar
 * (about 4rem); the negative margin lets the thread use the full width of the screen.
 */
export default function Chat() {
  const runtime = useChatRuntime();

  return (
    <div className="-mx-4 -mb-24 md:-mb-8 h-[calc(100dvh-3.5rem-4rem)] md:h-[calc(100dvh-3.5rem)]">
      <AssistantRuntimeProvider runtime={runtime}>
        <TooltipProvider>
          <ProductCardsUI />
          <Thread components={components} />
        </TooltipProvider>
      </AssistantRuntimeProvider>
    </div>
  );
}
