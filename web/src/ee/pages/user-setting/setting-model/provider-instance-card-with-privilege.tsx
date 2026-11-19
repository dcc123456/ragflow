import { Button } from '@/components/ui/button';
import { PrivilegeManagementDialog } from '@/ee/components/privilege-management/privilege-management-dialog';
import {
  Permission,
  PermissionResourceType,
  hasResourcePermission,
} from '@/ee/constants/team';
import {
  useFetchTenantInfo,
  useFetchUserInfo,
} from '@/hooks/use-user-setting-request';
import { IProviderInstance } from '@/interfaces/database/llm';
import { ProviderInstanceCard } from '@/pages/user-setting/setting-model/instance-card/provider-instance-card';
import { KeyRound } from 'lucide-react';
import {
  type ComponentProps,
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';
import { createPortal } from 'react-dom';
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
  const { data: userInfo } = useFetchUserInfo();
  const [permissionTarget, setPermissionTarget] =
    useState<PrivilegeDialogInitialValues | null>(null);
  const cardRef = useRef<HTMLDivElement>(null);
  const [permissionActionsContainer, setPermissionActionsContainer] =
    useState<HTMLElement | null>(null);
  const [instanceNameContainer, setInstanceNameContainer] =
    useState<HTMLElement | null>(null);

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
  const canManagePermissions = Boolean(
    tenantInfo?.tenant_id &&
    instance.id &&
    (hasResourcePermission(operatorPermission, Permission.Manage) ||
      hasResourcePermission(operatorPermission, Permission.Owner)),
  );

  useLayoutEffect(() => {
    setInstanceNameContainer(
      cardRef.current?.querySelector<HTMLElement>(
        '[data-testid="instance-name-row"]',
      ) ?? null,
    );
  }, [instance.id]);

  useLayoutEffect(() => {
    if (!canManagePermissions) {
      setPermissionActionsContainer(null);
      return;
    }

    const deleteButton = cardRef.current?.querySelector<HTMLButtonElement>(
      '[data-testid="instance-delete"]',
    );
    setPermissionActionsContainer(deleteButton?.parentElement ?? null);
  }, [canManagePermissions, instance.id]);

  return (
    <div ref={cardRef} className="relative">
      <ProviderInstanceCard
        providerName={providerName}
        instance={instance}
        defaultOpen={defaultOpen}
      />
      {instance.user_id &&
        instance.user_id !== userInfo?.id &&
        instance.nickname &&
        instanceNameContainer &&
        createPortal(
          <span
            className="shrink-0 truncate text-xs text-text-secondary"
            title={instance.nickname}
          >
            {instance.nickname}
          </span>,
          instanceNameContainer,
        )}
      {canManagePermissions &&
        permissionActionsContainer &&
        createPortal(
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="order-first"
            aria-label={t('permission.permissionManagement')}
            onClick={showInstancePermissions}
          >
            <KeyRound />
          </Button>,
          permissionActionsContainer,
        )}
      {permissionTarget && (
        <PrivilegeManagementDialog
          hideModal={() => setPermissionTarget(null)}
          initialValues={permissionTarget}
        />
      )}
    </div>
  );
}
