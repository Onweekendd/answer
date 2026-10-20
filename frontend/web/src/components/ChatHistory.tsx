import { LogOut, Menu, MessageCircle, Search, SquarePen } from "lucide-react";
import { useMemo, useState } from "react";

import { SectionSwitch } from "@/components/SectionSwitch";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { groupChats, searchChats } from "@/core/chat/groups";
import { useLogout } from "@/hooks/useAuth";
import { useChatList } from "@/hooks/useChatList";
import { cn } from "@/lib/utils";

/** The round, softly lifted buttons at the top of the chat. */
const floatButton =
  "size-10 rounded-full bg-background shadow-[0_2px_12px_rgb(0_0_0/0.08)] ring-1 ring-foreground/5 hover:bg-muted dark:shadow-none dark:ring-foreground/10";

/**
 * The chat's own top bar: the menu opens the drawer with the saved chats, the pen starts a new
 * one. The chat page has no site header, so the drawer is also the way to the community.
 */
export function ChatTopBar({ current, onOpen }: { current: string | null; onOpen: (id: string | null) => void }) {
  const [open, setOpen] = useState(false);

  return (
    <div className="relative z-10 flex items-center justify-between px-3 pt-[calc(env(safe-area-inset-top,0px)+0.5rem)] pb-1">
      <Button
        variant="ghost"
        size="icon"
        className={floatButton}
        aria-label="打开对话列表"
        onClick={() => setOpen(true)}
      >
        <Menu />
      </Button>
      <SectionSwitch />
      <Button variant="ghost" size="icon" className={floatButton} aria-label="新对话" onClick={() => onOpen(null)}>
        <SquarePen />
      </Button>

      <Sheet open={open} onOpenChange={setOpen}>
        {/* no initial focus on the search box: on a phone it would pop the keyboard over the list */}
        <SheetContent aria-describedby={undefined} initialFocus={false}>
          <ChatDrawer
            current={current}
            onPick={(id) => {
              setOpen(false);
              onOpen(id);
            }}
          />
        </SheetContent>
      </Sheet>
    </div>
  );
}

function ChatDrawer({ current, onPick }: { current: string | null; onPick: (id: string | null) => void }) {
  const [query, setQuery] = useState("");
  const { chats, isLoading, error } = useChatList(true);
  const logout = useLogout();
  const groups = useMemo(() => groupChats(searchChats(chats, query), new Date()), [chats, query]);

  return (
    <>
      <SheetTitle className="sr-only">对话列表</SheetTitle>
      <div className="flex flex-col gap-3 px-4 pt-[calc(env(safe-area-inset-top,0px)+1rem)]">
        <label className="flex h-11 items-center gap-2 rounded-full bg-muted px-4 text-muted-foreground focus-within:ring-2 focus-within:ring-ring/40">
          <Search className="size-4 shrink-0" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索对话"
            aria-label="搜索对话"
            className="min-w-0 flex-1 bg-transparent text-sm text-foreground outline-none placeholder:text-muted-foreground"
          />
        </label>
        <Button variant="ghost" className="h-10 justify-start gap-2 rounded-xl px-3" onClick={() => onPick(null)}>
          <SquarePen className="size-4" />
          新对话
        </Button>
      </div>

      <nav aria-label="保存的对话" className="mt-2 min-h-0 flex-1 overflow-y-auto px-2 pb-4">
        {isLoading && <p className="px-3 py-2 text-sm text-muted-foreground">加载中…</p>}
        {error && <p className="px-3 py-2 text-sm text-destructive">没取到，稍后再试</p>}
        {!isLoading && !error && groups.length === 0 && (
          <p className="px-3 py-2 text-sm text-muted-foreground">{query ? "没有找到" : "还没有保存的对话"}</p>
        )}
        {groups.map((group) => (
          <section key={group.label} className="mt-3 first:mt-1">
            <h3 className="px-3 pb-1 text-xs font-medium tracking-wide text-muted-foreground">{group.label}</h3>
            <ul className="flex flex-col">
              {group.chats.map((c) => (
                <li key={c.id}>
                  <button
                    type="button"
                    onClick={() => onPick(c.id)}
                    aria-current={c.id === current ? "page" : undefined}
                    className={cn(
                      "flex w-full items-center gap-2 rounded-xl px-3 py-2.5 text-left text-[15px] transition-colors hover:bg-muted",
                      c.id === current && "bg-accent text-accent-foreground hover:bg-accent"
                    )}
                  >
                    <MessageCircle className="size-4 shrink-0 opacity-50" />
                    <span className="min-w-0 flex-1 truncate">{c.title}</span>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </nav>

      <footer className="mx-3 mb-[calc(env(safe-area-inset-bottom,0px)+0.75rem)] flex items-center gap-1 rounded-2xl bg-muted/60 p-1.5">
        <span className="flex-1 px-3 text-sm text-muted-foreground">买不买</span>
        <ThemeToggle />
        <Button variant="ghost" size="icon" aria-label="退出登录" onClick={logout}>
          <LogOut />
        </Button>
      </footer>
    </>
  );
}
