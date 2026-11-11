import { useCallback, useMemo } from 'react';
import type { MouseEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router';

import { IconFontFill } from '@/components/icon-font';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { RAGFlowTooltip } from '@/components/ui/tooltip';
import { useSwitchTenant } from '@/ee/hooks/use-switch-tenant';
import { PriceName } from '@/ee/pages/price/constant';
import { useFetchCurrentPlan } from '@/ee/pages/price/hook/use-price-hooks';
import { PrivateRoutes } from '@/ee/private-routes';
import { isBillingEnabled } from '@/ee/services/billingStatus';
import {
  useFetchTenantInfo,
  useListTenant,
} from '@/hooks/use-user-setting-request';
import { ITenant } from '@/interfaces/database/user-setting';
import { cn } from '@/lib/utils';
import { TenantMembershipStatus } from '@/pages/user-setting/constants';
import { Building2, LucideChevronDown } from 'lucide-react';

const getTenantLabel = (tenant?: ITenant) =>
  tenant ? tenant.nickname || tenant.email || tenant.tenant_id : '';

export function EnterpriseHeaderTenantSwitcher({
  isCompact = false,
}: {
  isCompact?: boolean;
}) {
  const { t } = useTranslation();
  const { data: tenants } = useListTenant();
  const { data: tenantInfo } = useFetchTenantInfo();
  const { switching, switchToTenant } = useSwitchTenant();

  const currentTenantId = tenantInfo.tenant_id;
  const joinedTenants = useMemo(
    () =>
      tenants.filter(
        (tenant) => tenant.status === TenantMembershipStatus.Joined,
      ),
    [tenants],
  );
  const currentLabel = useMemo(() => {
    const currentTenant = joinedTenants.find(
      (tenant) => tenant.tenant_id === currentTenantId,
    );

    return getTenantLabel(currentTenant) || currentTenantId;
  }, [currentTenantId, joinedTenants]);

  if (joinedTenants.length === 0) {
    return null;
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className={cn(
            'h-10 shrink-0 text-text-primary',
            isCompact ? 'size-10 px-0' : 'ms-2 max-w-[220px] gap-1 px-2',
          )}
          aria-label={t('permission.switchTenant')}
          title={isCompact ? t('permission.switchTenant') : currentLabel}
          disabled={switching}
        >
          {isCompact ? (
            <Building2 className="size-5" />
          ) : (
            <>
              <span className="truncate">{currentLabel}</span>
              <LucideChevronDown className="size-[1em] shrink-0" />
            </>
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-w-[280px]">
        <DropdownMenuRadioGroup
          value={currentTenantId}
          onValueChange={switchToTenant}
        >
          {joinedTenants.map((tenant) => (
            <DropdownMenuRadioItem
              key={tenant.tenant_id}
              value={tenant.tenant_id}
              textValue={getTenantLabel(tenant)}
              className="max-w-[260px]"
            >
              <span className="truncate">{getTenantLabel(tenant)}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** Navigate to the pricing page; shared by the upgrade and plan badges. */
function useOpenPricePage() {
  const navigate = useNavigate();

  return useCallback(
    (event: MouseEvent) => {
      event.preventDefault();
      navigate(PrivateRoutes.Price);
    },
    [navigate],
  );
}

export function EnterpriseHeaderUpgradeBadge() {
  const { t } = useTranslation();
  const handleClick = useOpenPricePage();

  if (!isBillingEnabled()) return null;

  return (
    <div
      className="bg-gradient-to-r from-[#00BEB4] to-[#43FFA4] rounded-full px-2 py-1 text-sm  font-normal text-black cursor-pointer"
      onClick={handleClick}
    >
      {t('price.upgrade')}
    </div>
  );
}

export function EnterpriseHeaderTicketsLink() {
  const { t } = useTranslation();

  if (!isBillingEnabled()) return null;

  return (
    <RAGFlowTooltip tooltip={t('header.tickets')}>
      <Link
        className="p-2 text-text-secondary hover:text-text-primary focus-visible:text-text-primary"
        to={PrivateRoutes.Tickets}
      >
        <IconFontFill name="kefu" className="text-text-primary" />
      </Link>
    </RAGFlowTooltip>
  );
}

export function EnterpriseHeaderPlanBadge() {
  const { data: currentPlan } = useFetchCurrentPlan();
  const handleClick = useOpenPricePage();

  const planName = currentPlan?.plan_name;
  if (planName !== PriceName.Starter && planName !== PriceName.Pro) {
    return null;
  }

  return (
    <div
      className={cn(
        '-mt-1 z-20 bg-gradient-to-r from-[#00BEB4] to-[#43FFA4] rounded-full px-1 py-0.5 text-xs font-normal text-black cursor-pointer',
        planName === PriceName.Starter ? 'scale-90 -ml-1.5 ' : '-ml-1',
      )}
      onClick={handleClick}
    >
      {planName}
    </div>
  );
}
