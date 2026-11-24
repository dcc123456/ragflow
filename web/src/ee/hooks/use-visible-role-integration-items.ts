import { useTenantMyPermissions } from '@/ee/pages/user-setting/team/use-member-role-query';
import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';

type IntegrationItem = { id?: string; name?: string };

export function useVisibleRoleIntegrationItems<T extends IntegrationItem>(
  feature: 'model_provider' | 'data_source' | 'chat_channel',
  items: T[],
  getItemIDs: (item: T) => string[] = (item) => [item.id ?? item.name ?? ''],
): T[] {
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data: tenantPermissions } = useTenantMyPermissions(
    tenantInfo.tenant_id,
  );
  const permissions = tenantPermissions?.permissions;

  if (!permissions) return items;
  if (permissions[feature]?.enable !== true) return [];
  return items.filter((item) =>
    getItemIDs(item).some(
      (id) => permissions[`${feature}::${id}`]?.enable !== false,
    ),
  );
}
