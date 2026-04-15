import { IconFontFill } from '@/components/icon-font';
import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import ThemeSwitch from '@/components/theme-switch';
import { Button } from '@/components/ui/button';
import { Domain } from '@/constants/common';
import { useLogout } from '@/hooks/use-login-request';
import {
  useFetchEnableAdmin,
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

import {
  LucideBox,
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
  // {
  //   icon: MessageSquareQuote,
  //   label: 'Prompt Templates',
  //   key: Routes.Profile,
  // },
  // { icon: TextSearch, label: 'Retrieval Templates', key: Routes.Profile },
  // { icon: Cog, label: t('setting.system'), key: Routes.System },
  // { icon: Banknote, label: 'Plan', key: Routes.Plan },
];

export function SideBar() {
  const { data: userInfo } = useFetchUserInfo();
  const { handleMenuClick, active: activeItemKey } = useHandleMenuClick();
  const { version, fetchSystemVersion } = useFetchSystemVersion();
  const { t } = useTranslation();
  const { data: isAdmin } = useFetchIsAdmin();
  const { data: enableAdmin } = useFetchEnableAdmin();

  useEffect(() => {
    if (location.host !== Domain) {
      fetchSystemVersion();
    }
  }, [fetchSystemVersion]);
  const { logout } = useLogout();

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
        if (enableAdmin && isAdmin) {
          return true;
        }
        return false;
      }

      if (enableAdmin) {
        if (!isAdmin) {
          return x.key !== Routes.Model;
        }
      }

      return x;
    });
  }, [enableAdmin, isAdmin, t]);

  return (
    <aside className="w-[303px] bg-bg-base flex flex-col">
      <header>
        <h1 className="px-6 flex gap-2.5 items-center font-normal">
          <RAGFlowAvatar
            avatar={userInfo?.avatar}
            name={userInfo?.nickname}
            isPerson
          />

          <p className="text-sm text-text-primary">{userInfo?.email}</p>
        </h1>
      </header>

      <nav className="flex-1 overflow-auto mt-4 py-1">
        <ul className="px-6 flex flex-col gap-5">
          {items.map((item) => {
            const { key, icon, label, ...rest } = item;

            return (
              <li key={key}>
                <Button
                  {...rest}
                  block
                  variant="ghost"
                  className={cn(
                    'justify-start gap-2.5 px-3 relative h-10 text-base',
                    activeItemKey === key && 'bg-bg-card text-text-primary',
                  )}
                  onClick={handleMenuClick(key)}
                >
                  <section className="flex items-center gap-2.5">
                    {icon}
                    <span>{label}</span>
                  </section>
                  {/* {item.key === Routes.System && (
                    <div className="mr-2 px-2 bg-accent-primary-5 text-accent-primary rounded-md">
                      {version}
                    </div>
                  )} */}
                  {/* {active && (
                    <div className="absolute right-0 w-[5px] h-[66px] bg-primary rounded-l-xl shadow-[0_0_5.94px_#7561ff,0_0_11.88px_#7561ff,0_0_41.58px_#7561ff,0_0_83.16px_#7561ff,0_0_142.56px_#7561ff,0_0_249.48px_#7561ff]" />
                  )} */}
                </Button>
              </li>
            );
          })}
        </ul>
      </nav>

      <footer className="p-6 mt-auto">
        <div className="flex items-center gap-2 mb-6 justify-between">
          <span className="text-xs text-accent-primary">{version}</span>

          <ThemeSwitch />
        </div>

        <Button block size="lg" variant="transparent" onClick={() => logout()}>
          {t('setting.logout')}
        </Button>
      </footer>
    </aside>
  );
}
