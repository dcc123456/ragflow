import { QueryClient } from '@tanstack/react-query';

class QueryClientSingleton {
  private static instance: QueryClient | null = null;

  public static getInstance(): QueryClient {
    if (!QueryClientSingleton.instance) {
      QueryClientSingleton.instance = new QueryClient();
    }
    return QueryClientSingleton.instance;
  }

  public static setInstance(queryClient: QueryClient): void {
    QueryClientSingleton.instance = queryClient;
  }

  public static reset(): void {
    QueryClientSingleton.instance = null;
  }
}

export default QueryClientSingleton;
