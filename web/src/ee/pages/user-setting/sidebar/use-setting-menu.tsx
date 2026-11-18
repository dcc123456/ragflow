import {
  useFetchExposeApi,
  useFetchExposeModelProvider,
} from '@/ee/hooks/use-private-llm-request';
import { PriceName } from '@/ee/pages/price/constant';
import { useFetchCurrentPlan } from '@/ee/pages/price/hook/use-price-hooks';
import { PrivateRoutes } from '@/ee/private-routes';
import { isBillingEnabled } from '@/ee/services/billingStatus';
import { Routes } from '@/routes';
import { ReceiptText, Unplug } from 'lucide-react';
import { ReactNode, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

export type SettingMenuEntry = {
  icon: ReactNode;
  label: string;
  key: string;
  'data-testid'?: string;
};

/**
 * Extends the upstream settings menu with billing when it is active, and the
 * model provider entry when that page is exposed.
 */
export function useSettingMenuItems<T extends SettingMenuEntry>(
  items: T[],
): T[] {
  const { t } = useTranslation();
  const { data: exposeModelProvider } = useFetchExposeModelProvider();
  const { data: exposeApi } = useFetchExposeApi();
  const { data: currentPlan } = useFetchCurrentPlan();

  return useMemo(() => {
    const menus: SettingMenuEntry[] = [...items];

    if (isBillingEnabled()) {
      menus.splice(3, 0, {
        icon: <ReceiptText className="size-[1em]" />,
        label: t('setting.billing'),
        key: PrivateRoutes.Billing,
      });
    }

    if (exposeModelProvider) {
      menus.push({
        icon: <Unplug className="size-[1em]" />,
        label: t('setting.model'),
        key: Routes.Model,
      });
    }

    return menus.filter((x) => {
      if (x.key === Routes.Api) {
        if (
          exposeApi ||
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
        return exposeModelProvider;
      }

      return true;
    }) as T[];
  }, [items, t, currentPlan, exposeApi, exposeModelProvider]);
}
