import { useCallback } from 'react';
import type { MouseEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router';

import { IconFontFill } from '@/components/icon-font';
import { RAGFlowTooltip } from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';
import { PriceName } from '@/pages/price/constant';
import { useFetchCurrentPlan } from '@/pages/price/hook/use-price-hooks';
import { PrivateRoutes } from '@/private-routes';
import { isBillingEnabled } from '@/services/billingStatus';

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
