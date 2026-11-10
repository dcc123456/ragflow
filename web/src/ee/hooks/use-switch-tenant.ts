import { Authorization } from '@/constants/authorization';
import { switchTenant } from '@/ee/services/tenant-service';
import authorizationUtil from '@/utils/authorization-util';
import { useMutation } from '@tanstack/react-query';
import { AxiosHeaders } from 'axios';
import { useCallback } from 'react';

export const useSwitchTenant = () => {
  const { isPending, mutate } = useMutation({
    mutationKey: ['switchTenant'],
    mutationFn: async (tenantId: string) => {
      const response = await switchTenant(tenantId);
      const authorization = (response.headers as AxiosHeaders).get(
        Authorization,
      );

      return {
        code: response.data?.code,
        authorization: authorization ? String(authorization) : null,
      };
    },
    onSuccess: ({ authorization, code }) => {
      if (code !== 0 || !authorization) {
        return;
      }

      authorizationUtil.setAuthorization(authorization);
      window.location.reload();
    },
  });

  const switchToTenant = useCallback(
    (tenantId: string) => {
      mutate(tenantId);
    },
    [mutate],
  );

  return { switching: isPending, switchToTenant };
};
