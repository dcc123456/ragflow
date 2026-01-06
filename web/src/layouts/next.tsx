import React, { useEffect, useRef } from 'react';
import { Outlet } from 'react-router';
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
    <main ref={containerRef} className="h-full flex flex-col">
      <Header></Header>
      <Outlet />
    </main>
  );
}
