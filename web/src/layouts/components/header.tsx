import { IconFontFill } from '@/components/icon-font';
import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { RAGFlowTooltip } from '@/components/ui/tooltip';
import { useChangeLanguage } from '@/hooks/logic-hooks';
import {
  useFetchUserInfo,
  useListTenant,
} from '@/hooks/use-user-setting-request';
import { cn } from '@/lib/utils';
import { TenantRole } from '@/pages/user-setting/constants';
import { Routes } from '@/routes';
import { LucideChevronDown, LucideCircleHelp } from 'lucide-react';
import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useLocation, useNavigate } from 'react-router';
import { BellButton } from './bell-button';
import GlobalNavbar from './global-navbar';
import ThemeButton from './theme-button';

import { supportedLanguages } from '@/locales/config';
import { PriceName } from '@/pages/price/constant';
import { useFetchCurrentPlan } from '@/pages/price/hook/use-price-hooks';
import { PrivateRoutes } from '@/private-routes';
import { isBillingEnabled } from '@/services/billingStatus';

export function Header({
  className,
  ...props
}: React.HTMLAttributes<HTMLElement>) {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const changeLanguage = useChangeLanguage();

  const {
    data: { language = 'en', avatar, nickname },
  } = useFetchUserInfo();

  const { data: tenantData } = useListTenant();
  const hasNotification = useMemo(
    () => tenantData?.some((x) => x.role === TenantRole.Invite),
    [tenantData],
  );

  const currentLanguage = supportedLanguages.find((x) => x.code === language);
  const { data: currentPlan } = useFetchCurrentPlan();
  // const { openModal } = useUpgradeModal();

  const handleClick = (e: React.MouseEvent) => {
    e.preventDefault();
    // openModal();
    navigate(PrivateRoutes.Price);
  };

  // const langItems = LanguageList.map((x) => ({
  //   key: x,
  //   label: <span>{LanguageMap[x as keyof typeof LanguageMap]}</span>,
  // }));

  return (
    <header
      key="app-navbar"
      className={cn(
        'w-full grid grid-cols-[1fr_auto_1fr] grid-rows-1 items-center gap-8',
        className,
      )}
      {...props}
    >
      <div className="inline-flex items-center gap-3">
        <Link
          to={Routes.Root}
          aria-current={pathname === Routes.Root ? 'page' : undefined}
        >
          <img src={'/logo.svg'} alt="RAGFlow logo" className="size-10" />
        </Link>
        {isBillingEnabled() && (
          <div
            className="bg-gradient-to-r from-[#00BEB4] to-[#43FFA4] rounded-full px-2 py-1 text-sm  font-normal text-black cursor-pointer"
            onClick={handleClick}
          >
            {t('price.upgrade')}
          </div>
        )}
      </div>

      <GlobalNavbar />

      <div
        className="flex items-center justify-end gap-4 text-text-badge"
        data-testid="auth-status"
      >
        <a
          className="p-2 text-text-secondary hover:text-text-primary focus-visible:text-text-primary"
          target="_blank"
          href="https://discord.com/invite/NjYzJD3GM3"
          rel="noreferrer noopener"
        >
          <IconFontFill name="a-DiscordIconSVGVectorIcon" />
        </a>

        <a
          className="p-2 text-text-secondary hover:text-text-primary focus-visible:text-text-primary"
          target="_blank"
          href="https://github.com/infiniflow/ragflow"
          rel="noreferrer noopener"
        >
          <IconFontFill name="GitHub" />
        </a>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button className="flex items-center gap-1" variant="ghost">
              {currentLanguage?.displayName}
              <LucideChevronDown className="size-[1em]" />
            </Button>
          </DropdownMenuTrigger>

          <DropdownMenuContent>
            {supportedLanguages.map((x) => (
              <DropdownMenuItem
                key={x.code}
                onClick={() => changeLanguage(x.code)}
              >
                {x.displayName}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        <RAGFlowTooltip tooltip={t('header.tickets')}>
          <Link
            className="p-2 text-text-secondary hover:text-text-primary focus-visible:text-text-primary"
            to={Routes.Tickets}
          >
            <IconFontFill
              name={`kefu`}
              className="text-text-primary"
            ></IconFontFill>
          </Link>
          {/* <Tickets className="size-5" /> */}
        </RAGFlowTooltip>

        <Button
          asLink
          variant="ghost"
          size="icon"
          to="https://ragflow.io/docs/dev/category/user-guides"
          target="_blank"
          rel="noreferrer noopener"
        >
          <LucideCircleHelp className="size-[1em]" />
        </Button>

        <ThemeButton />

        {hasNotification && <BellButton />}

        <Link
          to={Routes.UserSetting}
          className="relative ms-3 flex items-start "
          data-testid="settings-entrypoint"
        >
          <RAGFlowAvatar
            name={nickname}
            avatar={avatar}
            isPerson
            className="size-8"
          />

          {(currentPlan?.plan_name === PriceName.Starter ||
            currentPlan?.plan_name === PriceName.Pro) && (
            <div
              className={cn(
                '-mt-1 z-20 bg-gradient-to-r from-[#00BEB4] to-[#43FFA4] rounded-full px-1 py-0.5 text-xs font-normal text-black cursor-pointer',
                currentPlan?.plan_name === 'Starter'
                  ? 'scale-90 -ml-1.5 '
                  : '-ml-1',
              )}
              onClick={handleClick}
            >
              <span className={cn()}>{currentPlan?.plan_name}</span>
            </div>
          )}
        </Link>
      </div>
    </header>
  );
}
