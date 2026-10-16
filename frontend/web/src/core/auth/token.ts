const KEY = "answer.token";

/** The slice of Storage we use, so tests can pass a plain object. */
export interface TokenStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface TokenStore {
  get(): string | null;
  set(token: string): void;
  clear(): void;
  isLoggedIn(): boolean;
  /** returns the unsubscribe function */
  subscribe(listener: () => void): () => void;
}

/**
 * Holds the login token in memory and mirrors it to storage. Storage can throw (private
 * windows, blocked site data), so every access is guarded and the memory copy keeps the
 * session working for the life of the page.
 */
export function createTokenStore(storage: TokenStorage): TokenStore {
  const listeners = new Set<() => void>();

  let token: string | null = null;
  try {
    token = storage.getItem(KEY);
  } catch {
    // keep the memory copy only
  }

  const emit = () => listeners.forEach((l) => l());

  return {
    get: () => token,
    set(next) {
      token = next;
      try {
        storage.setItem(KEY, next);
      } catch {
        // memory copy still holds it
      }
      emit();
    },
    clear() {
      if (token === null) {
        return;
      }
      token = null;
      try {
        storage.removeItem(KEY);
      } catch {
        // memory copy is already cleared
      }
      emit();
    },
    isLoggedIn: () => token !== null,
    subscribe(listener) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    }
  };
}
