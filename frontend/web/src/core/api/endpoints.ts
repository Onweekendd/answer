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

/** A saved message: the thread message's own JSON, and the id of the one it follows ("" for the first). */
export interface SavedChatMessage {
  parent_id: string;
  message: unknown;
}

export interface SavedChat {
  id: string;
  title: string;
  messages: SavedChatMessage[];
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

    chatGet: (id: string) => client.get<SavedChat>("/purchase/chat", { id }),

    chatCreate: (title: string) => client.post<{ id: string }>("/purchase/chat", { title }),

    /** Saving the same message id again overwrites it. */
    chatSaveMessage: (chatId: string, messageId: string, parentId: string | null, message: unknown) =>
      client.post<null>("/purchase/chat/message", {
        chat_id: chatId,
        message_id: messageId,
        parent_id: parentId ?? "",
        message
      }),

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
