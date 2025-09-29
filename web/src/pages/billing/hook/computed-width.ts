import { RefObject, useEffect, useState } from 'react';

export const useComputedWidth = (ref: RefObject<HTMLElement>) => {
  const [width, setWidth] = useState<number | null>(null);
  useEffect(() => {
    if (!ref || !window) {
      return;
    }
    if (!('ResizeObserver' in window)) {
      const handleResize = () => {
        if (ref.current) {
          setWidth(ref.current.offsetWidth);
        }
        console.log('width', ref);
      };

      window.addEventListener('resize', handleResize);

      handleResize();

      // clear Listener
      return () => {
        window.removeEventListener('resize', handleResize);
      };
    } else {
      const resizeObserver = new ResizeObserver((entries) => {
        for (let entry of entries) {
          setWidth(entry.contentRect.width);
        }
      });

      if (ref.current) {
        resizeObserver.observe(ref.current);
      }

      // clear ResizeObserver
      return () => {
        if (ref.current) {
          resizeObserver.unobserve(ref.current);
        }
      };
    }
  }, [ref]);
  return { width };
};
