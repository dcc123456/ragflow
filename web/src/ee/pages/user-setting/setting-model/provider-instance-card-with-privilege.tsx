import { Button } from '@/components/ui/button';
import { PrivilegeManagementDialog } from '@/ee/components/privilege-management/privilege-management-dialog';
import {
  Permission,
  PermissionResourceType,
  hasResourcePermission,
} from '@/ee/constants/team';
import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import { IProviderInstance } from '@/interfaces/database/llm';
import { ProviderInstanceCard } from '@/pages/user-setting/setting-model/instance-card/provider-instance-card';
import { KeyRound } from 'lucide-react';
import { type ComponentProps, useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';

type PrivilegeDialogInitialValues = ComponentProps<
  typeof PrivilegeManagementDialog
>['initialValues'];

type ProviderInstanceCardWithPrivilegeProps = {
  providerName: string;
  instance: IProviderInstance;
  defaultOpen: boolean;
};

/**
 * Wraps a provider instance card with the model-instance permission entry
 * point. Only managers and owners of the instance see the control.
 */
export function ProviderInstanceCardWithPrivilege({
  providerName,
  instance,
  defaultOpen,
}: ProviderInstanceCardWithPrivilegeProps) {
  const { t } = useTranslation();
  const { data: tenantInfo } = useFetchTenantInfo();
  const [permissionTarget, setPermissionTarget] =
    useState<PrivilegeDialogInitialValues | null>(null);

  const showInstancePermissions = useCallback(() => {
    const tenantId = tenantInfo?.tenant_id;
    if (!tenantId || !instance.id) return;
    setPermissionTarget({
      id: instance.id,
      tenant_id: tenantId,
      name: instance.instance_name,
      resourceType: PermissionResourceType.ModelInstance,
      operator_permission: instance.operator_permission,
    });
  }, [
    tenantInfo?.tenant_id,
    instance.id,
    instance.instance_name,
    instance.operator_permission,
  ]);

  const operatorPermission = instance.operator_permission ?? 0;
  const canManagePermissions =
    hasResourcePermission(operatorPermission, Permission.Manage) ||
    hasResourcePermission(operatorPermission, Permission.Owner);

  return (
    <div className="relative">
      {tenantInfo?.tenant_id && instance.id && canManagePermissions && (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          className="absolute right-3 top-3 z-20"
          aria-label={t('permission.permissionManagement')}
          onClick={showInstancePermissions}
        >
          <KeyRound />
        </Button>
      )}
      <ProviderInstanceCard
        providerName={providerName}
        instance={instance}
        defaultOpen={defaultOpen}
      />
      {permissionTarget && (
        <PrivilegeManagementDialog
          hideModal={() => setPermissionTarget(null)}
          initialValues={permissionTarget}
        />
      )}
    </div>
  );
}
