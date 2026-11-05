import { useMemo } from 'react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { LucideBox, LucideSettings } from 'lucide-react';

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
 * Enterprise-only admin navigation: settings, SSO providers and model
 * providers. The whitelist entry upstream ships is hidden until the
 * corresponding admin variable is enabled.
 */
export function useEnterpriseAdminNav(): AdminNavExtension {
  const { t } = useTranslation();
  const { variables } = useAdminVariables();

  const isWhitelistEnabled = !!variables?.enable_whitelist?.value;

  const items = useMemo<AdminNavItem[]>(
    () => [
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
    ],
    [t],
  );

  const hiddenPaths = useMemo(
    () => (isWhitelistEnabled ? [] : [Routes.AdminWhitelist]),
    [isWhitelistEnabled],
  );

  return { items, hiddenPaths };
}
