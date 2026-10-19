import { History, SquarePen } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useChatList } from "@/hooks/useChatList";
import { cn } from "@/lib/utils";

const when = (seconds: number) =>
  new Date(seconds * 1000).toLocaleString("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit"
  });

/** 「历史」opens the list of saved conversations; 「新对话」starts an empty one. */
export function ChatHistory({ current, onOpen }: { current: string | null; onOpen: (id: string | null) => void }) {
  const [open, setOpen] = useState(false);
  const { chats, isLoading, error } = useChatList(open);

  const pick = (id: string | null) => {
    setOpen(false);
    onOpen(id);
  };

  return (
    <div className="mx-auto flex w-full max-w-[44rem] justify-end gap-1 px-4 pt-2">
      <Button variant="ghost" size="sm" onClick={() => setOpen(true)}>
        <History />
        历史
      </Button>
      <Button variant="ghost" size="sm" onClick={() => pick(null)}>
        <SquarePen />
        新对话
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[80dvh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>历史对话</DialogTitle>
          </DialogHeader>
          {isLoading && <p className="text-sm text-muted-foreground">加载中…</p>}
          {error && <p className="text-sm text-destructive">没取到，稍后再试</p>}
          {!isLoading && !error && chats.length === 0 && (
            <p className="text-sm text-muted-foreground">还没有保存的对话</p>
          )}
          <ul className="flex flex-col gap-1">
            {chats.map((c) => (
              <li key={c.id}>
                <button
                  type="button"
                  onClick={() => pick(c.id)}
                  className={cn(
                    "flex w-full items-baseline gap-3 rounded-lg px-3 py-2 text-left text-sm hover:bg-accent",
                    c.id === current && "bg-accent"
                  )}
                >
                  <span className="min-w-0 flex-1 truncate">{c.title}</span>
                  <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{when(c.updated_at)}</span>
                </button>
              </li>
            ))}
          </ul>
        </DialogContent>
      </Dialog>
    </div>
  );
}
