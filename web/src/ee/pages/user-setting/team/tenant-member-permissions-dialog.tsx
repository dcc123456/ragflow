import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Switch } from '@/components/ui/switch';
import { ITenantManagementGrant } from '@/ee/interfaces/database/team';
import { ITenantUser } from '@/interfaces/database/user-setting';
import message from '@/components/ui/message';
import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSetTenantManagementPermissions } from './use-tenant-member-permissions';

const MANAGEMENT_PERMISSIONS = ['manage_members', 'manage_roles'] as const;

export function TenantMemberPermissionsDialog({
  tenantId,
  member,
  permissions,
  onClose,
}: {
  tenantId: string;
  member: ITenantUser;
  permissions: ITenantManagementGrant['permissions'];
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [draftPermissions, setDraftPermissions] =
    useState<ITenantManagementGrant['permissions']>(permissions);
  const { loading: saving, updatePermissions } =
    useSetTenantManagementPermissions(tenantId);

  useEffect(() => {
    setDraftPermissions(permissions);
  }, [permissions]);

  const changed = useMemo(() => {
    const initial = [...permissions].sort();
    const draft = [...draftPermissions].sort();

    return initial.join(',') !== draft.join(',');
  }, [draftPermissions, permissions]);

  const handleToggle = (
    permission: ITenantManagementGrant['permissions'][number],
    checked: boolean,
  ) => {
    setDraftPermissions((current) =>
      checked
        ? [...new Set([...current, permission])]
        : current.filter((value) => value !== permission),
    );
  };

  const handleSave = async () => {
    const code = await updatePermissions(member.user_id, draftPermissions);

    if (code === 0) {
      message.success(t('permission.tenantPermissionsUpdated'));
      onClose();
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[480px]">
        <DialogHeader>
          <DialogTitle>{t('permission.manageTenantPermissions')}</DialogTitle>
          <p className="truncate text-sm text-text-secondary">
            {member.nickname || member.email}
          </p>
        </DialogHeader>
        <div className="space-y-3">
          {MANAGEMENT_PERMISSIONS.map((permission) => (
            <div
              key={permission}
              className="flex items-center justify-between gap-4 rounded-lg border border-border-button px-4 py-3"
            >
              <span className="text-sm text-text-primary">
                {t(`permission.${permission}`)}
              </span>
              <Switch
                aria-label={t(`permission.${permission}`)}
                checked={draftPermissions.includes(permission)}
                onCheckedChange={(checked) => handleToggle(permission, checked)}
              />
            </div>
          ))}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button loading={saving} disabled={!changed} onClick={handleSave}>
            {t('common.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
