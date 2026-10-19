import { useState } from "react";

import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { cn } from "@/lib/utils";

/**
 * An image in an answer. A tap opens it over the page instead of leaving the chat; a tap on
 * the open image toggles between fitting the screen and twice the width, which scrolls.
 */
export function ZoomableImage({ src, alt, className }: { src: string; alt?: string; className?: string }) {
  const [open, setOpen] = useState(false);
  const [zoomed, setZoomed] = useState(false);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="my-3 block w-fit cursor-zoom-in"
        aria-label={alt ? `查看大图：${alt}` : "查看大图"}
      >
        <img
          className={cn("border-border max-h-[28rem] max-w-full rounded-lg border object-contain", className)}
          src={src}
          alt={alt ?? ""}
          loading="lazy"
        />
      </button>

      <Dialog
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          setZoomed(false);
        }}
      >
        <DialogContent className="max-w-[calc(100%-1rem)] p-2 sm:max-w-[min(95vw,72rem)]">
          <DialogTitle className="sr-only">{alt || "图片"}</DialogTitle>
          <div className="max-h-[85dvh] overflow-auto">
            <img
              src={src}
              alt={alt ?? ""}
              onClick={() => setZoomed((z) => !z)}
              className={cn(
                "mx-auto rounded-md",
                zoomed
                  ? "w-[200%] max-w-none cursor-zoom-out"
                  : "max-h-[85dvh] w-auto max-w-full cursor-zoom-in object-contain"
              )}
            />
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
