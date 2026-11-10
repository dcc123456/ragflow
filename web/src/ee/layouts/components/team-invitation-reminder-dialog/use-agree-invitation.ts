import {
  useAgreeTenant,
  useDeleteTenantUser,
  useFetchUserInfo,
} from '@/hooks/use-user-setting-request';
import { useCallback } from 'react';

/**
 * Accept an invitation: `PATCH /api/v1/tenants/{tenant_id}` joins the Tenant.
 * Declining removes the current user's own membership row.
 */
export const useAcceptTenantInvitation = () => {
  const { agreeTenant, loading: accepting } = useAgreeTenant();

  const acceptInvitation = useCallback(
    (tenantId: string) => {
      agreeTenant(tenantId);
    },
    [agreeTenant],
  );

  return { acceptInvitation, accepting };
};

export const useDeclineTenantInvitation = () => {
  const { deleteTenantUser, loading: declining } = useDeleteTenantUser();
  const { data: user } = useFetchUserInfo();

  const declineInvitation = useCallback(
    (tenantId: string) => {
      deleteTenantUser({ tenantId, userId: user.id });
    },
    [deleteTenantUser, user.id],
  );

  return { declineInvitation, declining };
};
