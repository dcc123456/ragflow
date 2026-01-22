import { Outlet, useLocation } from 'react-router';

import { cn } from '@/lib/utils';

import './index.less';

function UnsupportedFlipEffectBypassFallback({
  children,
}: React.PropsWithChildren) {
  return children;
}

function CSSFlipEffect({
  children,
  flipped,
}: React.PropsWithChildren<{ flipped: boolean }>) {
  return (
    <div className="relative size-full overflow-hidden will-change-transform [perspective:1000px] min-h-[680px] flex items-center">
      <div
        className={cn(
          'relative size-full [transform-style:preserve-3d] transition-transform duration-300 will-change-transform',
          flipped && 'rotate-y-180',
        )}
      >
        {/* Front Face */}
        <div className="absolute inset-0 flex items-center justify-center backface-hidden rotate-y-0">
          {children}
        </div>

        {/* Back Face */}
        <div className="absolute inset-0 flex items-center justify-center backface-hidden rotate-y-180">
          {children}
        </div>
      </div>
    </div>
  );
}

// It only depends on browser version, won't be altered in runtime
const CSS_SUPPORT_BACKFACE_HIDDEN =
  CSS.supports('backface-visibility', 'hidden') ||
  CSS.supports('-webkit-backface-visibility', 'hidden') ||
  CSS.supports('-moz-backface-visibility', 'hidden') ||
  CSS.supports('-ms-backface-visibility', 'hidden');

// Select the appropriate flip effect based on browser support
const FlipEffect = CSS_SUPPORT_BACKFACE_HIDDEN
  ? CSSFlipEffect
  : UnsupportedFlipEffectBypassFallback;

export default function FlipEffectContainer() {
  const location = useLocation();
  const isLoginPage = location.pathname === '/login';

  return (
    <FlipEffect flipped={isLoginPage}>
      <Outlet />
    </FlipEffect>
  );
}
