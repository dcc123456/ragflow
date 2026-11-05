import { QueryClientProvider } from '@tanstack/react-query';
import { ReactNode } from 'react';

import QueryClientSingleton from '@/ee/utils/query-client-singleton';

/**
 * Provider wrapped around portal modals, which render outside the app tree and
 * therefore outside its React Query provider. Open source renders children
 * directly.
 */
export function EnterpriseModalProvider({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={QueryClientSingleton.getInstance()}>
      {children}
    </QueryClientProvider>
  );
}
