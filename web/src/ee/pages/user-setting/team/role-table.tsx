import message from '@/components/ui/message';
import { ITenantRole } from '@/ee/interfaces/database/team';
import { ITenantUser } from '@/interfaces/database/user-setting';
import { TenantMembershipStatus } from '@/pages/user-setting/constants';
import { Routes } from '@/routes';
import { useMemo, useState } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { useTenantRoles } from './use-member-role-query';
import {
  useDeleteTenantRole,
  useSetTenantDefaultRole,
} from './use-tenant-role-management';
import { TenantRoleFormDialog } from './tenant-role-form-dialog';
import { RoleTableRow, RoleTableView } from './role-table-view';

export function RoleTable({
  keyword,
  tenantId,
  members,
  canManageRoles,
  onCreate,
  onKeywordChange,
}: {
  keyword: string;
  tenantId: string;
  members: ITenantUser[];
  canManageRoles: boolean;
  onCreate: () => void;
  onKeywordChange: (keyword: string) => void;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { data: roles, loading } = useTenantRoles(tenantId);
  const { loading: deleting, deleteRole } = useDeleteTenantRole(tenantId);
  const { loading: settingDefault, setDefaultRole } =
    useSetTenantDefaultRole(tenantId);
  const [editingRole, setEditingRole] = useState<ITenantRole>();

  const roleMemberCounts = useMemo(() => {
    const counts = new Map<number, number>();
    members.forEach((member) => {
      if (member.status !== TenantMembershipStatus.Joined) return;
      const roleId = Number(member.role);
      if (Number.isInteger(roleId) && roleId > 0) {
        counts.set(roleId, (counts.get(roleId) ?? 0) + 1);
      }
    });
    return counts;
  }, [members]);

  const tableRoles: RoleTableRow[] = roles.map((role) => ({
    id: role.id,
    role_name: role.role_name,
    description: role.description,
    is_default: role.is_default,
    count: roleMemberCounts.get(role.id) ?? 0,
  }));

  const handleSetDefaultRole = async (roleId: number) => {
    const code = await setDefaultRole(roleId);
    if (code === 0) message.success(t('permission.defaultRoleUpdated'));
  };

  const handleDeleteRole = async (role: RoleTableRow) => {
    const code = await deleteRole(role.id);
    if (code === 0) message.success(t('permission.roleDeleted'));
  };

  return (
    <>
      <RoleTableView
        keyword={keyword}
        roles={tableRoles}
        loading={loading}
        canManageRoles={canManageRoles}
        settingDefault={settingDefault}
        deleting={deleting}
        countLabel={t('permission.members')}
        toolbar
        onKeywordChange={onKeywordChange}
        onCreate={onCreate}
        onSetDefault={(roleId) => void handleSetDefaultRole(roleId)}
        onConfigure={(role) =>
          navigate(`${Routes.Team}/roles/${role.id}`, {
            state: {
              returnTo: {
                pathname: location.pathname,
                search: location.search,
                teamTab: 'role',
                keyword,
              },
            },
          })
        }
        onEdit={(role) =>
          setEditingRole(roles.find((item) => item.id === role.id))
        }
        onDelete={handleDeleteRole}
      />
      {editingRole && (
        <TenantRoleFormDialog
          tenantId={tenantId}
          role={editingRole}
          onClose={() => setEditingRole(undefined)}
        />
      )}
    </>
  );
}
