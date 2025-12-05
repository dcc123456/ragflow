import { TenantIdContext } from '@/contexts/teant-context';
import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import { useContext, useMemo } from 'react';

export function useIsMyCreatedTeam() {
  const currentTeamId = useContext(TenantIdContext);
  const { data: tenantInfo } = useFetchTenantInfo();

  const isMyCreatedTeam = useMemo(() => {
    return tenantInfo.tenant_id === currentTeamId;
  }, [currentTeamId, tenantInfo.tenant_id]);

  return isMyCreatedTeam;
}

export function useTenantId() {
  const tenantId = useContext(TenantIdContext);
  return tenantId;
}
