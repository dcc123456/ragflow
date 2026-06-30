import { useHeartBeat } from '@/hooks/use-heart-beat-request';
import { UpgradeModalProvider } from '@/pages/price/global';
import { isBillingEnabled } from '@/services/billingStatus';
import React from 'react';
import { Outlet, useLocation } from 'react-router';
import { Header } from './components/header';
import { NotificationBanner } from './components/notification-banner';

export const nextLayoutRef = React.createRef<HTMLDivElement>();

export function RootLayoutContainer({ children }: React.PropsWithChildren) {
  useHeartBeat();
  const { pathname } = useLocation();

  // useComputedRouterChangeCount();
  return (
    <div
      className="size-full grid grid-rows-[auto_auto_1fr] grid-cols-1 grid-flow-col"
      ref={nextLayoutRef}
    >
      <div>
        <NotificationBanner />
      </div>

      {pathname !== '/price' && <Header className="px-5 py-4" />}
      <main className="size-full min-w-0 overflow-hidden">{children}</main>
    </div>
  );
}

export default function RootLayout() {
  return (
    <RootLayoutContainer>
      {isBillingEnabled() && (
        <UpgradeModalProvider>
          <Outlet />
        </UpgradeModalProvider>
      )}
      {!isBillingEnabled() && <Outlet />}
    </RootLayoutContainer>
  );
}
