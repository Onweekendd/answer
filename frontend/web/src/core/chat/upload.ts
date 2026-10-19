import type { AttachmentAdapter, CompleteAttachment, PendingAttachment } from "@assistant-ui/react";

/**
 * Screenshots go to the server when the message is sent, and the message carries the url the
 * upload returns: the chat endpoint, like a post, only takes images uploaded to this site.
 * Until then the composer previews the local file, as the built-in image adapter does.
 */
export class UploadImageAdapter implements AttachmentAdapter {
  accept = "image/*";

  private readonly upload: (file: File) => Promise<string>;

  constructor(upload: (file: File) => Promise<string>) {
    this.upload = upload;
  }

  async add({ file }: { file: File }): Promise<PendingAttachment> {
    return {
      id: crypto.randomUUID(),
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
