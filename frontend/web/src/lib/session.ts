import { createApiClient } from "@/core/api/client";
import { createEndpoints } from "@/core/api/endpoints";
import { createTokenStore } from "@/core/auth/token";

/**
 * The app's single token store, API client and endpoint set. Hooks import from here; pages
 * never do. A 401 clears the token, which sends the router back to the login page.
 */
export const tokenStore = createTokenStore(window.localStorage);

export const apiClient = createApiClient({
  baseUrl: "/answer/api/v1",
  getToken: tokenStore.get,
  onUnauthorized: tokenStore.clear
});

export const api = createEndpoints(apiClient);
