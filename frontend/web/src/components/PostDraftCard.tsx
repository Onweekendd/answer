import { makeAssistantToolUI } from "@assistant-ui/react";
import { CheckCircle2Icon, SendIcon } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  type DraftForm,
  type DraftResult,
  formFromDraft,
  isDraftResult,
  isPostDraft,
  MAX_IMAGES,
  type PostDraft,
  PUBLISH_TOOL,
  toggleImage,
  VOTE_LABEL
} from "@/core/chat/draft";
import { usePublishDraft } from "@/hooks/usePublishDraft";
import { api } from "@/lib/session";
import { isOwnUpload } from "@/lib/uploads";
import { cn } from "@/lib/utils";

/**
 * The post draft the agent stopped at. The user edits it and publishes it to the community, or
 * drops it; either way the outcome becomes the tool call's result and is saved with the chat.
 */
export function PostDraftCard({
  draft,
  result,
  onDone
}: {
  draft: PostDraft;
  result: DraftResult | undefined;
  onDone: (result: DraftResult) => void;
}) {
  if (result?.status === "published") {
    return (
      <div className="my-3 flex items-center gap-2 rounded-xl border bg-card px-4 py-3 text-sm">
        <CheckCircle2Icon className="size-4 shrink-0 text-primary" />
        <span className="min-w-0 flex-1 truncate">已发到社区：{result.title}</span>
        {/* the post page belongs to the old site, outside this app's router */}
        <a href={`/questions/${result.questionId}`} className="shrink-0 text-primary underline underline-offset-2">
          去看看
        </a>
      </div>
    );
  }
  if (result?.status === "dropped") {
    return <p className="my-3 text-sm text-muted-foreground">已放弃这份帖子草稿。</p>;
  }
  return <DraftEditor draft={draft} onDone={onDone} />;
}

function DraftEditor({ draft, onDone }: { draft: PostDraft; onDone: (result: DraftResult) => void }) {
  const [form, setForm] = useState<DraftForm>(() => formFromDraft(draft));
  const { publish, pending, error } = usePublishDraft();
  const set = (patch: Partial<DraftForm>) => setForm((f) => ({ ...f, ...patch }));
  const pictures = draft.candidates.filter(isOwnUpload);
  const id = (field: string) => `draft-${draft.draftId}-${field}`;

  const submit = async () => {
    try {
      onDone(await publish({ draft, form }));
    } catch {
      // the hook keeps the error to show; the draft stays open for another try
    }
  };

  return (
    <section aria-label="帖子草稿" className="my-3 flex flex-col gap-3 rounded-xl border bg-card p-4 text-sm">
      <header className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="font-semibold">帖子草稿</h3>
        <span className="text-xs text-muted-foreground">发布后 AI 会先投一票：{VOTE_LABEL[draft.vote]}</span>
      </header>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor={id("title")}>标题</Label>
        <Input id={id("title")} value={form.title} onChange={(e) => set({ title: e.target.value })} />
      </div>

      <div className="flex flex-col gap-1.5">
        <span className="text-sm font-medium">
          配图{" "}
          <span className="text-xs font-normal text-muted-foreground">点选，最多 {MAX_IMAGES} 张，第一张是封面</span>
        </span>
        {pictures.length === 0 ? (
          <p className="text-xs text-muted-foreground">这段对话里还没有图，先在对话里发一张截图。</p>
        ) : (
          <div className="grid grid-cols-4 gap-2 sm:grid-cols-6">
            {pictures.map((url) => {
              const order = form.images.indexOf(url);
              return (
                <button
                  key={url}
                  type="button"
                  aria-pressed={order >= 0}
                  aria-label={order >= 0 ? `第 ${order + 1} 张配图` : "选这张图"}
                  onClick={() => set({ images: toggleImage(form.images, url) })}
                  className={cn(
                    "relative aspect-square overflow-hidden rounded-lg border-2",
                    order >= 0 ? "border-primary" : "border-transparent opacity-60"
                  )}
                >
                  <img src={url} alt="" loading="lazy" className="size-full object-cover" />
                  {order >= 0 && (
                    <span className="absolute top-1 left-1 rounded-full bg-primary px-1.5 text-xs text-primary-foreground tabular-nums">
                      {order + 1}
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        )}
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor={id("price")}>价格（元）</Label>
          <Input
            id={id("price")}
            inputMode="decimal"
            value={form.price}
            onChange={(e) => set({ price: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor={id("category")}>品类</Label>
          <Input id={id("category")} value={form.category} onChange={(e) => set({ category: e.target.value })} />
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor={id("link")}>商品链接</Label>
        <Input id={id("link")} value={form.link} onChange={(e) => set({ link: e.target.value })} />
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor={id("content")}>正文</Label>
        <Textarea id={id("content")} rows={6} value={form.content} onChange={(e) => set({ content: e.target.value })} />
      </div>

      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}

      <div className="flex justify-end gap-2">
        <Button
          variant="ghost"
          disabled={pending}
          onClick={() => {
            void api.draftResult(draft.draftId, { status: "dropped" }).catch(() => undefined);
            onDone({ status: "dropped" });
          }}
        >
          放弃
        </Button>
        <Button disabled={pending} onClick={submit}>
          <SendIcon />
          {pending ? "发布中…" : "发布到社区"}
        </Button>
      </div>
    </section>
  );
}

/**
 * Renders a draft part of an assistant message. Standalone: a decision for the user, it sits
 * below the answer instead of inside the folded process box.
 */
export const PostDraftUI = makeAssistantToolUI<Record<string, unknown>, unknown>({
  toolName: PUBLISH_TOOL,
  display: "standalone",
  render: ({ args, result, addResult }) =>
    isPostDraft(args) ? (
      <PostDraftCard draft={args} result={isDraftResult(result) ? result : undefined} onDone={(r) => addResult(r)} />
    ) : null
});
