import message from '@/components/ui/message';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Switch } from '@/components/ui/switch';
import {
  ITenantRole,
  ITenantRolePermissionMatrix,
} from '@/ee/interfaces/database/team';
import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useSetTenantRolePermissions,
  useTenantRolePermissions,
  useTenantRoleResources,
} from './use-tenant-role-management';

const ACTION_ORDER = ['enable', 'read', 'write', 'share'];

export function TenantRolePermissionsDialog({
  tenantId,
  role,
  onClose,
}: {
  tenantId: string;
  role: ITenantRole;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const {
    data: resources,
    loading: loadingResources,
    isError: resourcesError,
  } = useTenantRoleResources(tenantId, true);
  const { data: result, loading: loadingPermissions } =
    useTenantRolePermissions(tenantId, role.id, true);
  const { loading: saving, setPermissions } = useSetTenantRolePermissions(
    tenantId,
    role.id,
  );
  const [permissions, setPermissionsDraft] =
    useState<ITenantRolePermissionMatrix>({});

  useEffect(() => {
    if (result?.permissions) {
      setPermissionsDraft(result.permissions);
    }
  }, [result?.permissions]);

  const actions = useMemo(
    () =>
      ACTION_ORDER.filter((action) =>
        resources.some((resource) => resource.actions.includes(action)),
      ),
    [resources],
  );

  const handleToggle = (
    resourceName: string,
    action: string,
    checked: boolean,
    dependentActions: string[],
  ) => {
    setPermissionsDraft((current) => {
      const nextResourcePermissions = {
        ...current[resourceName],
        [action]: checked,
      };

      if (action === 'enable') {
        dependentActions.forEach((dependentAction) => {
          nextResourcePermissions[dependentAction] = false;
        });
      }

      return {
        ...current,
        [resourceName]: nextResourcePermissions,
      };
    });
  };

  const handleSave = async () => {
    const normalizedPermissions = Object.fromEntries(
      Object.entries(permissions).map(([resourceName, resourcePermissions]) => {
        const resource = resources.find(
          (item) => item.resource_type === resourceName,
        );
        if (
          !resource?.actions.includes('enable') ||
          resourcePermissions.enable
        ) {
          return [resourceName, resourcePermissions];
        }

        return [
          resourceName,
          {
            ...resourcePermissions,
            ...Object.fromEntries(
              resource.actions
                .filter((action) => action !== 'enable')
                .map((action) => [action, false]),
            ),
          },
        ];
      }),
    );
    const code = await setPermissions(normalizedPermissions);

    if (code === 0) {
      message.success(t('permission.rolePermissionsUpdated'));
      onClose();
    }
  };

  const loading = loadingResources || loadingPermissions;

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-[900px]">
        <DialogHeader>
          <DialogTitle>{t('permission.rolePermissions')}</DialogTitle>
          <p className="truncate text-sm text-text-secondary">
            {role.role_name}
          </p>
        </DialogHeader>
        <div className="max-h-[58vh] overflow-auto rounded-lg border border-border-button">
          {loading ? (
            <p className="p-6 text-center text-sm text-text-secondary">
              {t('common.loading')}
            </p>
          ) : resourcesError || !result ? (
            <p className="p-6 text-center text-sm text-state-error">
              {t('permission.rolePermissionsLoadFailed')}
            </p>
          ) : (
            <div className="min-w-[640px]">
              <div className="sticky top-0 z-10 grid grid-cols-[minmax(180px,1fr)_repeat(4,88px)] border-b border-border-button bg-bg-title px-4 py-3 text-sm text-text-secondary">
                <span>{t('permission.resource')}</span>
                {actions.map((action) => (
                  <span key={action} className="text-center">
                    {t(`admin.permissionType.${action}`)}
                  </span>
                ))}
                {Array.from({ length: 4 - actions.length }, (_, index) => (
                  <span key={`empty-${index}`} />
                ))}
              </div>
              {resources.map((resource) => (
                <div
                  key={resource.resource_type}
                  className="grid grid-cols-[minmax(180px,1fr)_repeat(4,88px)] items-center border-b border-border-button px-4 py-3 last:border-b-0"
                >
                  <span className="min-w-0 truncate pr-3 text-sm text-text-primary">
                    {t(`admin.resourceType.${resource.resource_type}`)}
                  </span>
                  {actions.map((action) => (
                    <div key={action} className="flex justify-center">
                      {resource.actions.includes(action) ? (
                        <Switch
                          aria-label={`${t(`admin.resourceType.${resource.resource_type}`)} ${t(`admin.permissionType.${action}`)}`}
                          checked={
                            action === 'enable'
                              ? !!permissions[resource.resource_type]?.[action]
                              : !!permissions[resource.resource_type]?.enable &&
                                !!permissions[resource.resource_type]?.[action]
                          }
                          disabled={
                            action !== 'enable' &&
                            !permissions[resource.resource_type]?.enable
                          }
                          onCheckedChange={(checked) =>
                            handleToggle(
                              resource.resource_type,
                              action,
                              checked,
                              resource.actions.filter(
                                (item) => item !== 'enable',
                              ),
                            )
                          }
                        />
                      ) : (
                        <span aria-hidden="true" className="text-text-disabled">
                          —
                        </span>
                      )}
                    </div>
                  ))}
                  {Array.from({ length: 4 - actions.length }, (_, index) => (
                    <span key={`empty-${index}`} />
                  ))}
                </div>
              ))}
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            loading={saving}
            disabled={loading || resourcesError || !result}
            onClick={handleSave}
          >
            {t('common.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
