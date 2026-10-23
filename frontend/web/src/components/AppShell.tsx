import { LogOut } from "lucide-react";
import { Outlet, useMatch } from "react-router-dom";

import { SectionSwitch } from "@/components/SectionSwitch";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Button } from "@/components/ui/button";
import { useLogout } from "@/hooks/useAuth";

/**
 * The frame of the signed-in pages. The 对话 | 社区 switch sits at the top on every page and
 * every screen size; the chat draws its own top bar around the same switch (ChatTopBar).
 */
export function AppShell() {
  const logout = useLogout();
  // The chat takes the whole screen, with its own top bar and drawer (ChatTopBar)
  const onChat = useMatch({ path: "/", end: true }) !== null;

  if (onChat) {
    return (
      <div className="h-dvh bg-background">
        <Outlet />
      </div>
    );
  }

  return (
    <div className="flex min-h-dvh flex-col bg-background">
      <header className="sticky top-0 z-20 bg-background/85 pt-[env(safe-area-inset-top,0px)] backdrop-blur">
        <div className="mx-auto grid h-16 w-full max-w-[1800px] grid-cols-[1fr_auto_1fr] items-center gap-2 px-4 md:px-8">
          <span className="text-base font-bold tracking-tight">买不买</span>
          <SectionSwitch />
          <div className="flex items-center justify-end gap-1">
            <ThemeToggle />
            <Button variant="ghost" size="icon" aria-label="退出登录" onClick={logout}>
              <LogOut />
            </Button>
          </div>
        </div>
      </header>

      <main className="mx-auto flex w-full max-w-[1800px] flex-1 flex-col px-4 pb-[calc(env(safe-area-inset-bottom,0px)+2rem)] md:px-8">
        <Outlet />
      </main>
    </div>
  );
}
