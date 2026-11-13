import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { ITenantRole } from '@/ee/interfaces/database/team';
import message from '@/components/ui/message';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useCreateTenantRole,
  useUpdateTenantRole,
} from './use-tenant-role-management';

export function TenantRoleFormDialog({
  tenantId,
  role,
  onClose,
}: {
  tenantId: string;
  role?: ITenantRole;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [roleName, setRoleName] = useState(role?.role_name ?? '');
  const [description, setDescription] = useState(role?.description ?? '');
  const { loading: creating, createRole } = useCreateTenantRole(tenantId);
  const { loading: updating, updateRole } = useUpdateTenantRole(
    tenantId,
    role?.id ?? 0,
  );
  const loading = creating || updating;

  useEffect(() => {
    setRoleName(role?.role_name ?? '');
    setDescription(role?.description ?? '');
  }, [role]);

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const payload = {
      role_name: roleName.trim(),
      description: description.trim(),
    };
    if (!payload.role_name || payload.role_name.length > 64) {
      return;
    }

    const code = role ? await updateRole(payload) : await createRole(payload);

    if (code === 0) {
      message.success(
        t(role ? 'permission.roleUpdated' : 'permission.roleCreated'),
      );
      onClose();
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[480px]">
        <DialogHeader>
          <DialogTitle>
            {t(role ? 'permission.editRole' : 'permission.createRole')}
          </DialogTitle>
        </DialogHeader>
        <form className="space-y-4" onSubmit={handleSubmit}>
          <div className="space-y-2">
            <Label htmlFor="tenant-role-name">{t('admin.roleName')}</Label>
            <Input
              id="tenant-role-name"
              autoComplete="off"
              required
              maxLength={64}
              value={roleName}
              onChange={(event) => setRoleName(event.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="tenant-role-description">
              {t('admin.description')}
            </Label>
            <Textarea
              id="tenant-role-description"
              rows={3}
              value={description}
              onChange={(event) => setDescription(event.target.value)}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button
              type="submit"
              loading={loading}
              disabled={!roleName.trim() || roleName.trim().length > 64}
            >
              {t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
