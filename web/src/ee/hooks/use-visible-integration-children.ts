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
  const permissions = tenantPermissions?.permissions;

  return useMemo(() => {
    if (!permissions) return children;

    const navigation =
      permissions.model_group?.enable === true
        ? addModelGroupIntegrationNavigation(children)
        : children;
    const featureByNavigationKey: Record<string, string> = {
      model: 'model_provider',
      'model-group': 'model_group',
      'data-source': 'data_source',
      'chat-channel': 'chat_channel',
      mcp: 'mcp',
    };

    return navigation.filter((child) => {
      const feature = featureByNavigationKey[child.key];
      return !feature || permissions[feature]?.enable !== false;
    });
  }, [children, permissions]);
}
