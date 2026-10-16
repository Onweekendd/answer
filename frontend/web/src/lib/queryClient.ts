import { QueryClient } from "@tanstack/react-query";

import { ApiError } from "@/core/api/client";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // a 4xx will not get better by asking again; only transient failures are retried
      retry: (count, err) => count < 2 && !(err instanceof ApiError && err.status >= 400 && err.status < 500)
    }
  }
});
