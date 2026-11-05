import { Outlet } from 'react-router';

import Spotlight from '@/components/spotlight';
import { useTranslate } from '@/hooks/common-hooks';
import { BgSvg } from '@/pages/login-next/bg';

const SPOTLIGHT_COLOR = 'rgb(128, 255, 248)';

export default function AuthLayout() {
  const { t } = useTranslate('login');

  return (
    <>
      <Spotlight opcity={0.4} coverage={60} color={SPOTLIGHT_COLOR} />

      <Spotlight
        opcity={0.3}
        coverage={12}
        X={'10%'}
        Y={'-10%'}
        color={SPOTLIGHT_COLOR}
      />

      <Spotlight
        opcity={0.3}
        coverage={12}
        X={'90%'}
        Y={'-10%'}
        color={SPOTLIGHT_COLOR}
      />

      <div className="h-[inherit] relative overflow-auto">
        <BgSvg isPaused />

        <header className="pt-3 mb-12 w-full text-text-primary">
          <div className="flex items-center mb-4 w-full px-14 pt-14 text-xl">
            <img
              className="size-8 mr-5 rounded-lg"
              src="/logo.svg"
              alt="logo"
            />

            <b>RAGFlow</b>
          </div>

          <h1 className="text-4xl font-medium  text-center mb-2">
            {t('title')}
          </h1>
        </header>

        <main className="relative z-10 min-h-[894px] px-4 sm:px-6 lg:px-8 flex justify-center">
          <Outlet />
        </main>
      </div>
    </>
  );
}
