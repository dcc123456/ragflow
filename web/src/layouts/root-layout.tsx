import { useHeartBeat } from '@/hooks/use-heart-beat-request';
import { useComputedRouterChangeCount } from '@/pages/price/gobal/hook';
import React from 'react';
import { Outlet } from 'react-router';
import { Header } from './components/header';

export const nextLayoutRef = React.createRef<HTMLDivElement>();

export function RootLayoutContainer({ children }: React.PropsWithChildren) {
  useHeartBeat();

  useComputedRouterChangeCount();
  return (
    <div
      className="size-full grid grid-rows-[auto_1fr] grid-cols-1 grid-flow-col"
      ref={nextLayoutRef}
    >
      <Header className="px-5 py-4" />

      <main className="size-full overflow-hidden">{children}</main>
    </div>
  );
}

export default function RootLayout() {
  return (
    <RootLayoutContainer>
      <Outlet />
    </RootLayoutContainer>
  );
}
