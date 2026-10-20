import { NavLink } from "react-router-dom";

import { cn } from "@/lib/utils";

/** 对话 and 社区, the two halves of the site, side by side at the top where they are seen first. */
export function SectionSwitch() {
  const tab = ({ isActive }: { isActive: boolean }) =>
    cn(
      "rounded-full px-4 py-1.5 text-sm font-medium transition-colors",
      isActive
        ? "bg-background text-foreground shadow-sm dark:bg-foreground/10"
        : "text-muted-foreground hover:text-foreground"
    );
  return (
    <nav aria-label="切换对话和社区" className="flex rounded-full bg-muted p-1">
      <NavLink to="/" end className={tab}>
        对话
      </NavLink>
      <NavLink to="/community" className={tab}>
        社区
      </NavLink>
    </nav>
  );
}
