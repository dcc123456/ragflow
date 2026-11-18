import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import { useCallback } from 'react';

type ModelGroupTenantAccessOptions = {
  ownerTenantId?: string;
  skipTenantInfo?: boolean;
  enabled?: boolean;
};

export function useModelGroupTenantAccess({
  ownerTenantId,
  skipTenantInfo,
  enabled = true,
}: ModelGroupTenantAccessOptions) {
  const tenantQueryEnabled =
    enabled && Boolean(ownerTenantId) && !skipTenantInfo;
  const {
    data: tenantInfo,
    isFetched,
    isError,
    refetch,
  } = useFetchTenantInfo(tenantQueryEnabled);
  const isSettled = !tenantQueryEnabled || isFetched;
  const canUseGroups =
    enabled &&
    !skipTenantInfo &&
    (!ownerTenantId ||
      (isFetched && !isError && tenantInfo.tenant_id === ownerTenantId));

  const resolveCanUseGroups = useCallback(async () => {
    if (!enabled || skipTenantInfo) return false;
    if (!ownerTenantId) return true;

    const result = await refetch();
    return !result.isError && result.data?.tenant_id === ownerTenantId;
  }, [enabled, ownerTenantId, refetch, skipTenantInfo]);

  return { canUseGroups, isSettled, resolveCanUseGroups };
}
