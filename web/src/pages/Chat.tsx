import { AssistantRuntimeProvider } from '@assistant-ui/react'
import { Thread } from '@/components/assistant-ui/thread.aui'
import { TooltipProvider } from '@/components/ui/tooltip'
import { useChatRuntime } from '@/hooks/useChatRuntime'

/**
 * The chat fills the space between the top bar (3.5rem) and, on phones, the bottom tab bar
 * (about 4rem); the negative margin lets the thread use the full width of the screen.
 */
export default function Chat() {
  const runtime = useChatRuntime()

  return (
    <div className="-mx-4 -mb-24 md:-mb-8 h-[calc(100dvh-3.5rem-4rem)] md:h-[calc(100dvh-3.5rem)]">
      <AssistantRuntimeProvider runtime={runtime}>
        <TooltipProvider>
          <Thread />
        </TooltipProvider>
      </AssistantRuntimeProvider>
    </div>
  )
}
