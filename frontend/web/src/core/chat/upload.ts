import type { AttachmentAdapter, CompleteAttachment, PendingAttachment } from "@assistant-ui/react";

/**
 * Screenshots go to the server when the message is sent, and the message carries the url the
 * upload returns: the chat endpoint, like a post, only takes images uploaded to this site.
 * Until then the composer previews the local file, as the built-in image adapter does.
 */
/**
 * An id for an attachment. crypto.randomUUID exists only in a secure context (https or
 * localhost): on the demo site, plain http on an IP, it is undefined, and picking an image
 * threw. getRandomValues has no such limit.
 */
export function attachmentId(): string {
  if (typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

export class UploadImageAdapter implements AttachmentAdapter {
  accept = "image/*";

  private readonly upload: (file: File) => Promise<string>;

  constructor(upload: (file: File) => Promise<string>) {
    this.upload = upload;
  }

  async add({ file }: { file: File }): Promise<PendingAttachment> {
    return {
      id: attachmentId(),
      type: "image",
      name: file.name,
      contentType: file.type,
      file,
      status: { type: "requires-action", reason: "composer-send" }
    };
  }

  async send(attachment: PendingAttachment): Promise<CompleteAttachment> {
    const url = await this.upload(attachment.file);
    return { ...attachment, status: { type: "complete" }, content: [{ type: "image", image: url }] };
  }

  async remove(): Promise<void> {}
}
