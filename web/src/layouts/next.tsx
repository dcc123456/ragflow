import { UpgradeModalProvider } from '@/pages/price/gobal';
import React, { useEffect, useRef } from 'react';
import { Outlet } from 'umi';
import { Header } from './next-header';

export let nextLayoutRef = React.createRef<HTMLDivElement>();

export default function NextLayout() {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    nextLayoutRef = containerRef;

    return () => {
      nextLayoutRef = null as any;
    };
  }, []);

  return (
    <section ref={containerRef} className="h-full flex flex-col">
      <Header></Header>

      <UpgradeModalProvider>
        <Outlet />
      </UpgradeModalProvider>
    </section>
  );
}
