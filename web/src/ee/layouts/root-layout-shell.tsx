import { useHeartBeat } from '@/ee/hooks/use-heart-beat-request';
import { NotificationBanner } from '@/ee/layouts/components/notification-banner';
import { UpgradeModalProvider } from '@/ee/pages/price/global';
import { isBillingEnabled } from '@/ee/services/billingStatus';
import React, { PropsWithChildren, useMemo } from 'react';
import { useLocation } from 'react-router';

export const nextLayoutRef = React.createRef<HTMLDivElement>();

export type RootLayoutChrome = {
  showHeader: boolean;
  containerRef?: React.Ref<HTMLDivElement>;
};

/** Enterprise notification banner rendered above the upstream header. */
export function RootLayoutBanner() {
  return <NotificationBanner />;
}

/** Wraps routed content in the billing upgrade modal provider. */
export function RootLayoutContent({ children }: PropsWithChildren) {
  return isBillingEnabled() ? (
    <UpgradeModalProvider>{children}</UpgradeModalProvider>
  ) : (
    <>{children}</>
  );
}

/** The pricing page ships its own hero, so the shared header is hidden there. */
export function useRootLayoutChrome(): RootLayoutChrome {
  useHeartBeat();
  const { pathname } = useLocation();

  return useMemo(
    () => ({ showHeader: pathname !== '/price', containerRef: nextLayoutRef }),
    [pathname],
  );
}
