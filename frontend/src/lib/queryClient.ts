import { QueryClient } from '@tanstack/react-query';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Polling keeps boards live-ish while several people (and agents)
      // work the same lists (TASK-46): refetch every 15s in the
      // background and immediately when the tab regains focus. While the
      // live-update stream is connected, lib/realtime.ts slows this to 2
      // minutes.
      staleTime: 10_000,
      refetchInterval: 15_000,
      refetchOnWindowFocus: true,
      retry: 1,
    },
    mutations: {
      retry: 0,
    },
  },
});
