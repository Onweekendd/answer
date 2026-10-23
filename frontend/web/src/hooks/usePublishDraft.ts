import { useMutation } from "@tanstack/react-query";

import { ApiError } from "@/core/api/client";
import { describeError, fieldErrors } from "@/core/api/errors";
import { checkDraftForm, type DraftForm, type DraftResult, type PostDraft, toQuestionAdd } from "@/core/chat/draft";
import { api } from "@/lib/session";

/** A form problem found before anything is sent; its message is meant for the user. */
class DraftFormError extends Error {}

/**
 * Publishes a draft through the usual post endpoint, then asks the AI to cast its vote first.
 * The post is what matters: when only the vote fails, the post still counts as published.
 */
export function usePublishDraft() {
  const mutation = useMutation({
    mutationFn: async ({ draft, form }: { draft: PostDraft; form: DraftForm }): Promise<DraftResult> => {
      const problem = checkDraftForm(form);
      if (problem) {
        throw new DraftFormError(problem);
      }
      const post = await api.publishPost(toQuestionAdd(form));
      await api.draftVote(draft.draftId, post.id).catch(() => undefined);
      const result: DraftResult = { status: "published", questionId: post.id, title: form.title.trim() };
      // the chat remembers it so the history shows the draft as published; the post is out either way
      await api.draftResult(draft.draftId, result).catch(() => undefined);
      return result;
    }
  });

  return { publish: mutation.mutateAsync, pending: mutation.isPending, error: messageOf(mutation.error) };
}

function messageOf(err: Error | null): string | null {
  if (!err) {
    return null;
  }
  if (err instanceof DraftFormError) {
    return err.message;
  }
  if (err instanceof ApiError) {
    return Object.values(fieldErrors(err))[0] ?? describeError(err);
  }
  return describeError(err);
}
