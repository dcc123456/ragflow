import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { LucideStar } from 'lucide-react';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import {
  getDefaultTenantSystemRole,
  setDefaultTenantSystemRole,
} from '@/ee/services/default-tenant-role-service';
import type { DefaultTenantRoleSetting } from '@/ee/services/default-tenant-role-service';

const DefaultTenantRoleKeys = {
  current: () => ['admin', 'default-tenant-role'] as const,
};

export function useDefaultTenantRole() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const {
    data: setting,
    isError: isSettingError,
    isPending: isSettingPending,
    isFetching: isSettingFetching,
  } = useQuery({
    queryKey: DefaultTenantRoleKeys.current(),
    queryFn: async () => {
      const { data } = await getDefaultTenantSystemRole();
      if (data.code !== 0) {
        throw new Error(data.message || t('admin.defaultTenantRoleLoadFailed'));
      }
      return data.data;
    },
    retry: false,
  });

  const updateDefaultRole = useMutation({
    mutationFn: async (roleId: number) => {
      const { data } = await setDefaultTenantSystemRole(roleId);
      if (data.code !== 0) {
        throw new Error(
          data.message || t('admin.defaultTenantRoleUpdateFailed'),
        );
      }
      return data.data;
    },
    onSuccess: async (nextSetting) => {
      queryClient.setQueryData<DefaultTenantRoleSetting>(
        DefaultTenantRoleKeys.current(),
        nextSetting,
      );
      toast.success(t('admin.defaultTenantRoleUpdated'));
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('admin.defaultTenantRoleUpdateFailed'),
      );
    },
  });

  return {
    defaultRoleId: setting?.role_id ?? undefined,
    isLoading: isSettingPending || isSettingFetching,
    isError: isSettingError,
    isUpdating: updateDefaultRole.isPending,
    setDefaultRole: updateDefaultRole.mutate,
  };
}

export function DefaultTenantRoleAction({
  isDefault,
  disabled,
  onSelect,
}: {
  isDefault: boolean;
  disabled: boolean;
  onSelect: () => void;
}) {
  const { t } = useTranslation();

  return (
    <Button
      variant={isDefault ? 'secondary' : 'ghost'}
      size="icon"
      className={
        isDefault ? 'shrink-0 text-accent-primary disabled:opacity-100' : ''
      }
      aria-label={
        isDefault
          ? t('admin.defaultTenantRole')
          : t('admin.setAsDefaultTenantRole')
      }
      aria-pressed={isDefault}
      disabled={disabled || isDefault}
      onClick={onSelect}
    >
      <LucideStar
        aria-hidden="true"
        className={isDefault ? 'size-4 fill-current' : 'size-4'}
      />
    </Button>
  );
}

export function DefaultTenantRoleLoadError({ isError }: { isError: boolean }) {
  const { t } = useTranslation();

  if (!isError) {
    return null;
  }

  return (
    <p role="alert" className="text-sm text-state-error">
      {t('admin.defaultTenantRoleLoadFailed')}
    </p>
  );
}
