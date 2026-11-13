import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import { TruncatedText } from '@/components/truncated-text';
import { TableEmpty, TableSkeleton } from '@/components/table-skeleton';
import { Button } from '@/components/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import message from '@/components/ui/message';
import { ITenantRole } from '@/ee/interfaces/database/team';
import { ITenantUser } from '@/interfaces/database/user-setting';
import { Pencil, ShieldCheck, Star, Trash2 } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useTenantRoles } from './use-member-role-query';
import {
  useDeleteTenantRole,
  useSetTenantDefaultRole,
} from './use-tenant-role-management';
import { TenantRoleFormDialog } from './tenant-role-form-dialog';
import { TenantRolePermissionsDialog } from './tenant-role-permissions-dialog';

export function RoleTable({
  keyword,
  tenantId,
  members,
  canManageRoles,
}: {
  keyword: string;
  tenantId: string;
  members: ITenantUser[];
  canManageRoles: boolean;
}) {
  const { t } = useTranslation();
  const { data: roles, loading } = useTenantRoles(tenantId);
  const { loading: deleting, deleteRole } = useDeleteTenantRole(tenantId);
  const { loading: settingDefault, setDefaultRole } =
    useSetTenantDefaultRole(tenantId);
  const [editingRole, setEditingRole] = useState<ITenantRole>();
  const [permissionsRole, setPermissionsRole] = useState<ITenantRole>();
  const normalizedKeyword = keyword.trim().toLowerCase();

  const visibleRoles = useMemo(() => {
    if (!normalizedKeyword) {
      return roles;
    }

    return roles.filter(
      (role) =>
        role.role_name.toLowerCase().includes(normalizedKeyword) ||
        role.description?.toLowerCase().includes(normalizedKeyword),
    );
  }, [normalizedKeyword, roles]);

  const roleMemberCounts = useMemo(() => {
    const counts = new Map<number, number>();
    members.forEach((member) => {
      const roleId = Number(member.role);
      if (Number.isInteger(roleId) && roleId > 0) {
        counts.set(roleId, (counts.get(roleId) ?? 0) + 1);
      }
    });
    return counts;
  }, [members]);

  const handleSetDefaultRole = async (roleId: number) => {
    const code = await setDefaultRole(roleId);

    if (code === 0) {
      message.success(t('permission.defaultRoleUpdated'));
    }
  };

  const handleDeleteRole = async (roleId: number) => {
    const code = await deleteRole(roleId);

    if (code === 0) {
      message.success(t('permission.roleDeleted'));
    }
  };

  return (
    <>
      <Table rootClassName="max-h-[68vh]">
        <TableHeader>
          <TableRow>
            <TableHead>{t('admin.roleName')}</TableHead>
            <TableHead>{t('admin.description')}</TableHead>
            <TableHead className="w-[150px]">
              {t('permission.members')}
            </TableHead>
            <TableHead className="w-[88px] text-center">
              {t('permission.defaultRole')}
            </TableHead>
            <TableHead className="w-[148px] text-right">
              {t('common.action')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <TableSkeleton columnsLength={5} />
          ) : visibleRoles.length > 0 ? (
            visibleRoles.map((role) => {
              const memberCount = roleMemberCounts.get(role.id) ?? 0;
              const cannotDelete = role.is_default || memberCount > 0;

              return (
                <TableRow key={role.id} className="group">
                  <TableCell className="max-w-[240px]">
                    <TruncatedText
                      as="span"
                      className="block truncate"
                      tooltip={role.role_name}
                    >
                      {role.role_name}
                    </TruncatedText>
                  </TableCell>
                  <TableCell className="max-w-[360px]">
                    <TruncatedText
                      as="span"
                      className="block truncate text-text-secondary"
                      tooltip={role.description || ''}
                    >
                      {role.description || '—'}
                    </TruncatedText>
                  </TableCell>
                  <TableCell>
                    {t('permission.membersWithRole', { count: memberCount })}
                  </TableCell>
                  <TableCell className="text-center">
                    <Button
                      variant={role.is_default ? 'secondary' : 'ghost'}
                      size="icon"
                      className={
                        role.is_default
                          ? 'text-accent-primary disabled:opacity-100'
                          : ''
                      }
                      aria-label={
                        role.is_default
                          ? t('admin.defaultTenantRole')
                          : t('admin.setAsDefaultTenantRole')
                      }
                      aria-pressed={role.is_default}
                      title={
                        role.is_default
                          ? t('admin.defaultTenantRole')
                          : t('admin.setAsDefaultTenantRole')
                      }
                      disabled={
                        !canManageRoles || role.is_default || settingDefault
                      }
                      onClick={() => void handleSetDefaultRole(role.id)}
                    >
                      <Star
                        className={
                          role.is_default ? 'size-4 fill-current' : 'size-4'
                        }
                      />
                    </Button>
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center justify-end gap-1">
                      {canManageRoles && (
                        <>
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label={t('permission.rolePermissions')}
                            title={t('permission.rolePermissions')}
                            onClick={() => setPermissionsRole(role)}
                          >
                            <ShieldCheck />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label={t('permission.editRole')}
                            title={t('permission.editRole')}
                            onClick={() => setEditingRole(role)}
                          >
                            <Pencil />
                          </Button>
                          <ConfirmDeleteDialog
                            title={t('permission.deleteRole')}
                            okButtonText={t('permission.deleteRole')}
                            onOk={() => handleDeleteRole(role.id)}
                            content={{
                              node: (
                                <ConfirmDeleteDialogNode
                                  name={role.role_name}
                                  warnText={t('permission.deleteRoleWarning')}
                                />
                              ),
                            }}
                          >
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              disabled={cannotDelete || deleting}
                              aria-label={t('permission.deleteRole')}
                              title={
                                role.is_default
                                  ? t('admin.defaultTenantRole')
                                  : memberCount > 0
                                    ? t('permission.roleInUse')
                                    : t('permission.deleteRole')
                              }
                            >
                              <Trash2 />
                            </Button>
                          </ConfirmDeleteDialog>
                        </>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              );
            })
          ) : (
            <TableEmpty columnsLength={5} />
          )}
        </TableBody>
      </Table>
      {editingRole && (
        <TenantRoleFormDialog
          tenantId={tenantId}
          role={editingRole}
          onClose={() => setEditingRole(undefined)}
        />
      )}
      {permissionsRole && (
        <TenantRolePermissionsDialog
          tenantId={tenantId}
          role={permissionsRole}
          onClose={() => setPermissionsRole(undefined)}
        />
      )}
    </>
  );
}
