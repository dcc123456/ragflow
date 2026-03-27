'use client';

import { PrivilegeAvatar } from '@/components/privilege-management/privilege-avatar';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Permission, TeamRole } from '@/constants/team';
import { KeyRound, Search } from 'lucide-react';
import { createContext, useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useFilteredPermissions,
  usePermissionData,
  usePermissionUpdate,
} from './hooks';
import {
  IPermissionManagementDialogProps,
  PermissionFilter,
} from './interface';
import { PermissionTable } from './permission-table';

export const PermissionManagementDialogContext = createContext<
  | ((target: {
      id: string;
      name: string;
      avatar?: string;
      email?: string;
      role: TeamRole;
    }) => void)
  | null
>(null);

const permissionFilters: {
  value: PermissionFilter;
  label: string;
  permission: number | null;
}[] = [
  { value: 'manage', label: 'manage', permission: Permission.Manage },
  { value: 'write', label: 'write', permission: Permission.Write },
  { value: 'read', label: 'read', permission: Permission.Read },
  { value: 'invisible', label: 'invisible', permission: 0 },
];

export function PermissionManagementDialog({
  hideModal,
  initialValues,
}: IPermissionManagementDialogProps) {
  const { t } = useTranslation();
  const [searchKeyword, setSearchKeyword] = useState('');
  const [activeFilter, setActiveFilter] = useState<PermissionFilter>('all');
  const [changedPermissions, setChangedPermissions] = useState<
    Record<string, number>
  >({});

  const { permissions, loading, updatePermissionLocal } = usePermissionData(
    initialValues.tenant_id,
    initialValues.id,
    initialValues.role,
  );

  const { handleUpdatePermission, loading: updating } = usePermissionUpdate(
    initialValues.tenant_id,
    initialValues.id,
    initialValues.role,
  );

  const filteredPermissions = useFilteredPermissions(
    permissions,
    searchKeyword,
    activeFilter,
  );

  const handlePermissionChange = useCallback(
    (kbId: string, permission: number) => {
      updatePermissionLocal(kbId, permission);
      setChangedPermissions((prev) => ({
        ...prev,
        [kbId]: permission,
      }));
    },
    [updatePermissionLocal],
  );

  const handleConfirm = useCallback(async () => {
    // Apply all changed permissions
    const updates = Object.entries(changedPermissions);
    for (const [kbId, permission] of updates) {
      await handleUpdatePermission([kbId], permission);
    }
    hideModal();
  }, [changedPermissions, handleUpdatePermission, hideModal]);

  const displaySubtitle = initialValues.email
    ? initialValues.email
    : initialValues.name;

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex gap-3 items-center text-xl">
            <KeyRound className="size-5" />
            {t('permission.permissionManagement')}
          </DialogTitle>
        </DialogHeader>

        {/* Description */}
        <p className="text-sm text-muted-foreground -mt-2">
          {t('permission.permissionManagementDescription')}
        </p>

        {/* User Info */}
        <div className="flex items-center gap-3 py-2">
          <PrivilegeAvatar avatar={initialValues.avatar} className="size-10" />
          <span className="text-lg font-medium">{displaySubtitle}</span>
        </div>

        {/* Search */}
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
          <Input
            placeholder={t('common.search')}
            value={searchKeyword}
            onChange={(e) => setSearchKeyword(e.target.value)}
            className="pl-10"
          />
        </div>

        {/* Permission Filter Tabs */}
        <div className="flex gap-2 flex-wrap">
          {permissionFilters.map((filter) => (
            <Button
              key={filter.value}
              variant={activeFilter === filter.value ? 'default' : 'outline'}
              size="sm"
              onClick={() =>
                setActiveFilter(
                  activeFilter === filter.value ? 'all' : filter.value,
                )
              }
            >
              {t(`permission.${filter.label}`)}
            </Button>
          ))}
        </div>

        {/* Permission Table */}
        <PermissionTable
          data={filteredPermissions}
          onPermissionChange={handlePermissionChange}
        />

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={hideModal}>
            {t('common.cancel')}
          </Button>
          <Button onClick={handleConfirm} disabled={updating || loading}>
            {t('common.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
