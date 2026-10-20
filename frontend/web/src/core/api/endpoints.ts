import type { toQuestionAdd } from "../chat/draft";
import type { Choice, MyChoice } from "../vote/tally";
import type { ApiClient } from "./client";

/** The part of the server's UserLoginResp we use. */
export interface LoginResp {
  id: string;
  username: string;
  display_name?: string;
  avatar?: string;
  access_token: string;
  /** 1 verified, 2 waiting for the verification mail */
  mail_status: number;
}

export interface PurchaseInfo {
  images: string[];
  price: number;
  currency?: string;
  link?: string;
  buy_count?: number;
  wait_count?: number;
  skip_count?: number;
  vote_closed?: boolean;
  /** the viewer's own choice; the feed does not fill it yet */
  my_vote?: MyChoice;
  ai_vote?: MyChoice;
}

/** The fields of a post that the community cards use. */
export interface FeedItem {
  id: string;
  title: string;
  url_title: string;
  description: string;
  purchase?: PurchaseInfo;
}

export interface VoteResp {
  buy_count: number;
  wait_count: number;
  skip_count: number;
  my_choice: Choice;
  closed: boolean;
}

/** One row of the conversation list; updated_at is unix seconds. */
export interface ChatItem {
  id: string;
  title: string;
  updated_at: number;
}

/**
 * One message of a conversation as the server rebuilds it from its log: the user's words and
 * images, or a turn's answer as the events it streamed, to replay. `id` is the logged entry, so
 * a loaded message is already known to the server; `parent_id` is the page message it follows.
 */
export interface PageMessage {
  id: string;
  parent_id: string;
  role: "user" | "assistant";
  text?: string;
  images?: string[];
  events?: unknown[];
  /** unix seconds */
  created_at: number;
}

/** Registering may leave the account waiting for its verification mail. */
export const needsEmailVerification = (resp: LoginResp) => resp.mail_status === 2 || !resp.access_token;

export function createEndpoints(client: ApiClient) {
  return {
    login: (email: string, password: string) =>
      client.post<LoginResp>("/user/login/email", { e_mail: email, pass: password }),

    register: (name: string, email: string, password: string) =>
      client.post<LoginResp>("/user/register/email", { name, e_mail: email, pass: password }),

    communityFeed: async (signal?: AbortSignal) => {
      const list = await client.get<FeedItem[] | null>("/community/feed", undefined, signal ? { signal } : undefined);
      return list ?? [];
    },

    vote: (questionId: string, choice: Choice) =>
      client.post<VoteResp>("/purchase/vote", { question_id: questionId, choice }),

    chatList: async (signal?: AbortSignal) => {
      const list = await client.get<ChatItem[] | null>("/purchase/chats", undefined, signal ? { signal } : undefined);
      return list ?? [];
    },

    /** Every message of the conversation, every branch; the server logs them as the turns run. */
    chatHistory: async (chatId: string) =>
      (await client.get<PageMessage[] | null>("/purchase/ai/chat/history", { chat_id: chatId })) ?? [],

    chatCreate: (title: string) => client.post<{ id: string }>("/purchase/chat", { title }),

    /** Publishes a post; the usual post rules apply (tags, rank, captcha after many posts). */
    publishPost: (body: ReturnType<typeof toQuestionAdd>) => client.post<{ id: string }>("/question", body),

    /** Asks the AI to cast the vote it kept with the draft, on the post just published from it. */
    draftVote: (draftId: string, questionId: string) =>
      client.post<null>("/purchase/ai/draft/vote", { draft_id: draftId, question_id: questionId }),

    /** Uploads an image for a post and returns its url. */
    uploadImage: (file: File) => {
      const form = new FormData();
      form.append("source", "post");
      form.append("file", file);
      return client.upload<string>("/file", form);
    }
  };
}

export type Endpoints = ReturnType<typeof createEndpoints>;
