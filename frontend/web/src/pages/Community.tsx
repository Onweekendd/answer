import { ArrowUpRight, RefreshCw } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";

import { PostCard } from "@/components/PostCard";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { describeError } from "@/core/api/errors";
import { useFeed } from "@/hooks/useFeed";
import { useVote } from "@/hooks/useVote";
import { cn } from "@/lib/utils";

const masonry =
  "columns-1 gap-6 min-[700px]:columns-2 min-[1000px]:columns-3 min-[1300px]:columns-4 min-[1600px]:columns-5";

export default function Community() {
  const feed = useFeed();
  const vote = useVote();
  const [selected, setSelected] = useState("");
  const categories = [
    ...new Map(feed.items.flatMap((item) => item.tags ?? []).map((tag) => [tag.slug_name, tag])).values()
  ];
  // A refreshed random batch may no longer contain the selected tag.
  const category = categories.some((tag) => tag.slug_name === selected) ? selected : "";
  const items = category
    ? feed.items.filter((item) => item.tags?.some((tag) => tag.slug_name === category))
    : feed.items;

  return (
    <div className="space-y-6 py-5 md:py-7">
      <div className="flex items-end justify-between gap-4">
        <div className="space-y-1.5">
          <h1 className="text-2xl font-semibold tracking-tight">逛逛大家的纠结</h1>
          <p className="text-sm text-muted-foreground">心动的东西，听听大家怎么选。</p>
        </div>
        <Link to="/" className="flex shrink-0 items-center gap-1 text-sm font-medium text-primary hover:underline">
          我也想问问
          <ArrowUpRight className="size-4" />
        </Link>
      </div>

      <div className="flex min-w-0 items-center gap-3 border-b border-border/60 pb-3">
        <nav aria-label="筛选本批帖子" className="flex min-w-0 flex-1 gap-2 overflow-x-auto py-1">
          <button
            type="button"
            aria-pressed={!category}
            onClick={() => setSelected("")}
            className={cn(
              "shrink-0 rounded-full px-4 py-2 text-sm transition-colors",
              !category ? "bg-foreground font-medium text-background" : "text-muted-foreground hover:bg-muted"
            )}
          >
            推荐
          </button>
          {categories.map((tag) => (
            <button
              key={tag.slug_name}
              type="button"
              aria-pressed={category === tag.slug_name}
              onClick={() => setSelected(tag.slug_name)}
              className={cn(
                "shrink-0 rounded-full px-4 py-2 text-sm transition-colors",
                category === tag.slug_name
                  ? "bg-foreground font-medium text-background"
                  : "text-muted-foreground hover:bg-muted"
              )}
            >
              {tag.display_name}
            </button>
          ))}
        </nav>
        <Button variant="ghost" disabled={feed.isLoading || feed.isRefreshing} onClick={() => feed.nextBatch()}>
          <RefreshCw className={feed.isRefreshing ? "animate-spin" : undefined} />
          换一批
        </Button>
      </div>

      {feed.error && !feed.isLoading && (
        <div role="alert" className="space-y-3 py-8 text-center text-sm text-muted-foreground">
          <p>{describeError(feed.error)}</p>
          <Button variant="outline" onClick={() => feed.nextBatch()}>
            重试
          </Button>
        </div>
      )}

      {!feed.isLoading && !feed.error && feed.items.length === 0 && (
        <div className="space-y-4 py-20 text-center">
          <p className="text-muted-foreground">还没有待投票的纠结，来聊聊你想买的东西？</p>
          <Link to="/" className="text-sm text-primary hover:underline">
            发起第一个讨论 →
          </Link>
        </div>
      )}

      <div className={masonry} aria-busy={feed.isLoading || feed.isRefreshing}>
        {feed.isLoading &&
          Array.from({ length: 10 }, (_, i) => (
            <div key={i} className="mb-7 break-inside-avoid space-y-3" aria-hidden="true">
              <Skeleton className={cn("w-full rounded-2xl", i % 2 ? "aspect-[4/5]" : "aspect-[3/4]")} />
              <Skeleton className="h-4 w-4/5" />
              <Skeleton className="h-4 w-1/2" />
            </div>
          ))}
        {items.map((item) => (
          <PostCard
            key={item.id}
            item={item}
            pending={vote.isPending && vote.variables?.id === item.id}
            onVote={(choice) =>
              vote.mutate({ id: item.id, choice }, { onError: (err) => toast.error(describeError(err)) })
            }
          />
        ))}
      </div>
      {items.length > 0 && (
        <p className="pb-3 text-center text-xs text-muted-foreground">这批看完了？点「换一批」发现更多纠结。</p>
      )}
    </div>
  );
}
