import { IconFontFill } from '@/components/icon-font';
import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import ThemeSwitch from '@/components/theme-switch';
import { Button } from '@/components/ui/button';
import { useLogout } from '@/hooks/use-login-request';
import {
  useFetchEnableAdmin,
  useFetchExposeModelProvider,
  useFetchIsAdmin,
} from '@/hooks/use-private-llm-request';
import {
  useFetchSystemVersion,
  useFetchUserInfo,
} from '@/hooks/use-user-setting-request';
import { cn } from '@/lib/utils';
import { PrivateRoutes } from '@/private-routes';
import { Routes } from '@/routes';
import { isBillingEnabled } from '@/services/billingStatus';
import { TFunction } from 'i18next';

import { PriceName } from '@/pages/price/constant';
import { useFetchCurrentPlan } from '@/pages/price/hook/use-price-hooks';
import {
  LucideBox,
  LucideMessagesSquare,
  LucideLogOut,
  LucideServer,
  LucideUnplug,
  LucideUser,
  LucideUsers,
  ReceiptText,
} from 'lucide-react';
import { useEffect, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { useHandleMenuClick } from './hooks';
type MenuItem = {
  icon: any;
  label: string;
  key: Routes | PrivateRoutes;
  'data-testid'?: string;
};
// const menuItems = (t: TFunction): MenuItem[] => [
//   { icon: Server, label: t('setting.dataSources'), key: Routes.DataSource },
//   { icon: Box, label: t('setting.model'), key: Routes.Model },
//   { icon: Banknote, label: 'MCP', key: Routes.Mcp },
//   { icon: Users, label: t('setting.team'), key: Routes.Team },
//   { icon: User, label: t('setting.profile'), key: Routes.Profile },
//   { icon: Unplug, label: t('setting.api'), key: Routes.Api },

const menuItems = (t: TFunction): MenuItem[] => [
  {
    icon: <LucideServer className="size-[1em]" />,
    label: t('setting.dataSources'),
    key: Routes.DataSource,
  },
  {
    icon: <LucideMessagesSquare className="size-[1em]" />,
    label: t('setting.chatChannels'),
    key: Routes.ChatChannel,
  },
  {
    icon: <LucideBox className="size-[1em]" />,
    label: t('setting.model'),
    key: Routes.Model,
    'data-testid': 'settings-nav-model-providers',
  },
  {
    icon: <IconFontFill name="mcp" className="size-[1em]" />,
    label: 'MCP',
    key: Routes.Mcp,
  },
  {
    icon: <LucideUsers className="size-[1em]" />,
    label: t('setting.team'),
    key: Routes.Team,
  },
  {
    icon: <LucideUser className="size-[1em]" />,
    label: t('setting.profile'),
    key: Routes.Profile,
  },
  {
    icon: <LucideUnplug className="size-[1em]" />,
    label: t('setting.api'),
    key: Routes.Api,
  },
];

export function SideBar() {
  const { data: userInfo } = useFetchUserInfo();
  const { handleMenuClick, active: activeItemKey } = useHandleMenuClick();
  const { version, fetchSystemVersion } = useFetchSystemVersion();
  const { t } = useTranslation();
  const { data: isAdmin } = useFetchIsAdmin();
  const { data: enableAdmin } = useFetchEnableAdmin();
  const { data: exposeModelProvider } = useFetchExposeModelProvider();

  useEffect(() => {
    fetchSystemVersion();
  }, [fetchSystemVersion]);
  const { logout } = useLogout();
  const { data: currentPlan } = useFetchCurrentPlan();

  const items = useMemo(() => {
    const menus: MenuItem[] = [...menuItems(t)];
    if (isBillingEnabled()) {
      const billingMenuItem: MenuItem = {
        icon: <ReceiptText className="size-[1em]" />,
        label: t('setting.billing'),
        key: PrivateRoutes.Billing,
      };
      menus.splice(3, 0, billingMenuItem);
    }
    return menus.filter((x) => {
      if (x.key === Routes.Api) {
        if (
          (enableAdmin && isAdmin) ||
          (currentPlan?.plan_name &&
            [PriceName.Starter, PriceName.Pro].includes(
              currentPlan.plan_name as PriceName,
            ))
        ) {
          return true;
        }
        return false;
      }

      if (x.key === Routes.Model) {
        if (exposeModelProvider) {
          return true;
        }
        if (enableAdmin && !isAdmin) {
          return false;
        }
      }

      return x;
    });
  }, [enableAdmin, isAdmin, exposeModelProvider, t, currentPlan]);

  return (
    <aside className="shrink-0 w-16 md:w-[303px] bg-bg-base flex flex-col overflow-hidden">
      <header>
        <h1 className="px-2 md:px-6 flex gap-2.5 items-center justify-center md:justify-start font-normal">
          <RAGFlowAvatar
            avatar={userInfo?.avatar}
            name={userInfo?.nickname}
            isPerson
          />

          <p className="hidden md:block text-sm text-text-primary truncate">
            {userInfo?.email}
          </p>
        </h1>
      </header>

      <nav className="flex-1 overflow-auto mt-4 py-1">
          <ul className="px-2 md:px-6 flex flex-col gap-2 md:gap-5 items-center md:items-stretch">
          {items.map((item) => {
            const { key, icon, label, ...rest } = item;

            return (
              <li key={key} className="w-full md:w-auto">
                <Button
                  {...rest}
                  block
                  variant="ghost"
                  aria-label={label}
                  className={cn(
                    'relative h-10 text-base max-md:size-10 max-md:p-0 max-md:justify-center justify-start gap-2.5 px-2 md:px-3',
                    activeItemKey === key && 'bg-bg-card text-text-primary',
                  )}
                  onClick={handleMenuClick(key)}
                >
                  <span className="flex items-center gap-2.5 max-md:gap-0">
                    {icon}
                    <span className="hidden md:inline">{label}</span>
                  </span>
                </Button>
              </li>
            );
          })}
        </ul>
      </nav>

      <footer className="p-2 md:p-6 mt-auto">
        <div className="hidden md:flex items-center gap-2 mb-6 justify-between">
          <span className="text-xs text-accent-primary">{version}</span>

          <ThemeSwitch />
        </div>

        <Button
          block
          size="lg"
          variant="transparent"
          aria-label={t('setting.logout')}
          className="max-md:size-10 max-md:p-0 max-md:mx-auto max-md:justify-center"
          onClick={() => logout()}
        >
          <LucideLogOut className="size-[1em] md:hidden" />
          <span className="hidden md:inline">{t('setting.logout')}</span>
        </Button>
      </footer>
    </aside>
  );
}
