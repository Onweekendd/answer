import { RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { PostCard } from "@/components/PostCard";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { describeError } from "@/core/api/errors";
import { useFeed } from "@/hooks/useFeed";
import { useVote } from "@/hooks/useVote";

function CardSkeleton() {
  return (
    <Card>
      <CardContent className="space-y-4">
        <Skeleton className="h-5 w-2/3" />
        <Skeleton className="h-4 w-full" />
        <div className="grid grid-cols-3 gap-2">
          <Skeleton className="aspect-square" />
          <Skeleton className="aspect-square" />
          <Skeleton className="aspect-square" />
        </div>
        <Skeleton className="h-10 w-full" />
      </CardContent>
    </Card>
  );
}

export default function Community() {
  const feed = useFeed();
  const vote = useVote();

  return (
    <div className="space-y-4 py-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">社区</h1>
        <Button variant="outline" disabled={feed.isLoading || feed.isRefreshing} onClick={() => feed.nextBatch()}>
          <RefreshCw className={feed.isRefreshing ? "animate-spin" : undefined} />
          换一批
        </Button>
      </div>

      {feed.isLoading && Array.from({ length: 3 }, (_, i) => <CardSkeleton key={i} />)}

      {feed.error && !feed.isLoading && (
        <div className="space-y-3 py-12 text-center text-sm text-muted-foreground">
          <p>{describeError(feed.error)}</p>
          <Button variant="outline" onClick={() => feed.nextBatch()}>
            重试
          </Button>
        </div>
      )}

      {!feed.isLoading && !feed.error && feed.items.length === 0 && (
        <p className="py-16 text-center text-sm text-muted-foreground">暂时没有可以投票的帖子，晚点再来看看</p>
      )}

      {feed.items.map((item) => (
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
  );
}
