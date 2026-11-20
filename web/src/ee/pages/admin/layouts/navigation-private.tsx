import { useMemo } from 'react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Building2,
  LucideBox,
  LucideChartColumn,
  LucideMonitor,
  LucideSettings,
  LucideSquareUserRound,
  LucideUserStar,
  LucideZap,
} from 'lucide-react';

import { LucideCloudKey } from '@/ee/pages/admin/components/icons/LucideCloudKey';
import useAdminVariables from '@/ee/pages/admin/hooks/useAdminVariables';
import { PrivateRoutes } from '@/ee/private-routes';
import { Routes } from '@/routes';

export type AdminNavItem = {
  path: string;
  name: string;
  icon: ReactNode;
};

export type AdminNavExtension = {
  items: AdminNavItem[];
  hiddenPaths: string[];
};

/**
 * Enterprise admin navigation keeps tenant management beside Users and adds
 * the remaining enterprise-only admin pages.
 */
export function useEnterpriseAdminNav(): AdminNavExtension {
  const { t } = useTranslation();
  const { variables } = useAdminVariables();

  const isWhitelistEnabled = !!variables?.enable_whitelist?.value;

  const items = useMemo<AdminNavItem[]>(
    () => [
      {
        path: PrivateRoutes.AdminTenants,
        name: t('admin.tenants'),
        icon: <Building2 className="size-[1em]" />,
      },
      {
        path: Routes.AdminSandboxSettings,
        name: t('admin.sandboxSettings'),
        icon: <LucideZap className="size-[1em]" />,
      },
      ...(isWhitelistEnabled
        ? [
            {
              path: Routes.AdminWhitelist,
              name: t('admin.registrationWhitelist'),
              icon: <LucideUserStar className="size-[1em]" />,
            },
          ]
        : []),
      {
        path: Routes.AdminRoles,
        name: t('admin.roles'),
        icon: <LucideSquareUserRound className="size-[1em]" />,
      },
      {
        path: Routes.AdminMonitoring,
        name: t('admin.monitoring'),
        icon: <LucideMonitor className="size-[1em]" />,
      },
      {
        path: PrivateRoutes.AdminSettings,
        name: t('admin.settings'),
        icon: <LucideSettings className="size-[1em]" />,
      },
      {
        path: PrivateRoutes.AdminSSOProviders,
        name: t('admin.ssoProvider'),
        icon: <LucideCloudKey className="size-[1em]" />,
      },
      {
        path: PrivateRoutes.AdminModelProviders,
        name: t('admin.modelProviders'),
        icon: <LucideBox className="size-[1em]" />,
      },
      {
        path: PrivateRoutes.AdminModelUsageStatistics,
        name: t('admin.modelUsage.modelUsageStatistics'),
        icon: <LucideChartColumn className="size-[1em]" />,
      },
    ],
    [isWhitelistEnabled, t],
  );

  const hiddenPaths = useMemo(
    () => [
      Routes.AdminSandboxSettings,
      Routes.AdminWhitelist,
      Routes.AdminRoles,
      Routes.AdminMonitoring,
    ],
    [],
  );

  return { items, hiddenPaths };
}
