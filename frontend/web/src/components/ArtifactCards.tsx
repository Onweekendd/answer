import { makeAssistantDataUI } from "@assistant-ui/react";
import { DownloadIcon, ExternalLinkIcon, FileTextIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";

import { buttonVariants } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { formatSize } from "@/core/chat/artifact";
import { type Artifact, ARTIFACTS_PART } from "@/core/chat/mapper";
import { cn } from "@/lib/utils";

/**
 * What the frame lets the page do. The page was written by the model from what it read on other
 * sites, so it must not act as our page: without allow-same-origin it runs as an opaque origin and
 * cannot read the user's login or call our API. Scripts may run (tabs, sorting), and its source
 * links open in a new tab as the real site. The server sends the same rule as a CSP header for when
 * the page is opened on its own; the two together cover a header that goes missing.
 */
const FRAME_SANDBOX = "allow-scripts allow-popups allow-popups-to-escape-sandbox";

/**
 * The pages the agent made for this answer, right below it: the thing the user asked for. A card
 * shows the page's screenshot, not the page, so a chat with several of them stays light; tapping it
 * opens the page itself.
 */
export function ArtifactCards({ artifacts }: { artifacts: readonly Artifact[] }) {
  if (artifacts.length === 0) {
    return null;
  }
  return (
    <section aria-label="交付的文件" className="mt-4 flex flex-col gap-1.5">
      <h3 className="text-xs text-muted-foreground">交付的文件</h3>
      <ul className="grid gap-2 sm:grid-cols-2">
        {artifacts.map((artifact) => (
          <li key={artifact.id}>
            <ArtifactCard artifact={artifact} />
          </li>
        ))}
      </ul>
    </section>
  );
}

function ArtifactCard({ artifact }: { artifact: Artifact }) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="group flex w-full flex-col overflow-hidden rounded-xl border bg-card text-left text-card-foreground transition-colors hover:bg-muted/60"
      >
        <div className="relative aspect-[16/10] w-full overflow-hidden border-b bg-muted">
          {artifact.image ? (
            <img
              src={artifact.image}
              alt=""
              loading="lazy"
              className="size-full object-cover object-top transition-transform group-hover:scale-[1.02]"
            />
          ) : (
            <FileTextIcon className="absolute inset-0 m-auto size-8 text-muted-foreground" aria-hidden />
          )}
        </div>
        <div className="flex flex-col gap-0.5 p-2.5">
          <p className="truncate text-sm font-medium">{artifact.title}</p>
          <p className="text-xs text-muted-foreground tabular-nums">网页 · {formatSize(artifact.size)}</p>
        </div>
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="flex h-[90dvh] max-w-[calc(100%-1rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-[min(95vw,72rem)]">
          <ArtifactPreview artifact={artifact} />
        </DialogContent>
      </Dialog>
    </>
  );
}

/** The open page: a bar with its title and what the user can do with it, then the page in its frame. */
function ArtifactPreview({ artifact }: { artifact: Artifact }) {
  const [loaded, setLoaded] = useState(false);
  const action = cn(buttonVariants({ variant: "outline", size: "sm" }));

  return (
    <>
      {/* pr-12 leaves room for the dialog's close button in the corner */}
      <div className="flex flex-wrap items-center gap-2 border-b py-2 pr-12 pl-3">
        <DialogTitle className="min-w-0 flex-1 truncate text-sm font-medium">{artifact.title}</DialogTitle>
        {artifact.image && (
          <a href={artifact.image} download={`${artifact.title}.png`} className={action}>
            <DownloadIcon aria-hidden />
            保存为图片
          </a>
        )}
        <a href={artifact.url} target="_blank" rel="noopener noreferrer" className={action}>
          <ExternalLinkIcon aria-hidden />
          在新标签页打开
        </a>
      </div>
      <div className="relative min-h-0 flex-1 bg-white">
        {!loaded && (
          <Loader2Icon className="absolute inset-0 m-auto size-6 animate-spin text-muted-foreground" aria-hidden />
        )}
        <iframe
          src={artifact.url}
          title={artifact.title}
          sandbox={FRAME_SANDBOX}
          referrerPolicy="no-referrer"
          onLoad={() => setLoaded(true)}
          className="size-full border-0"
        />
      </div>
    </>
  );
}

/** Renders the message's artifacts part; mount it inside the runtime provider. */
export const ArtifactCardsUI = makeAssistantDataUI<Artifact[]>({
  name: ARTIFACTS_PART,
  render: ({ data }) => <ArtifactCards artifacts={data} />
});
