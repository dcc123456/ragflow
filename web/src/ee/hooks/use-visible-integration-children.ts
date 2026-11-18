import { addModelGroupIntegrationNavigation } from '@/ee/layouts/components/app-sidebar/navigation-private';
import { useTenantMyPermissions } from '@/ee/pages/user-setting/team/use-member-role-query';
import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import type { IntegrationChild } from '@/layouts/components/app-sidebar/navigation';
import { useMemo } from 'react';

export function useVisibleIntegrationChildren(
  children: IntegrationChild[],
): IntegrationChild[] {
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data: tenantPermissions } = useTenantMyPermissions(
    tenantInfo.tenant_id,
  );
  const canUseModelGroups =
    tenantPermissions?.permissions?.model_provider?.enable === true;

  return useMemo(
    () =>
      canUseModelGroups
        ? addModelGroupIntegrationNavigation(children)
        : children,
    [canUseModelGroups, children],
  );
}
