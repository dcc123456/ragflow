import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router';

import { RoleFormDialog } from '@/ee/pages/user-setting/team/role-form-dialog';
import message from '@/components/ui/message';
import { PrivateRoutes } from '@/ee/private-routes';
import { RoleTableView } from '@/ee/pages/user-setting/team/role-table-view';
import type { RoleTableRow } from '@/ee/pages/user-setting/team/role-table-view';
import {
  deleteRole as deleteAdminRole,
  updateRoleDescription,
} from '@/ee/services/admin-service';
import { useCreateRole, useRoleList } from '@/ee/pages/admin/hooks/useRole';
import {
  DefaultTenantRoleLoadError,
  useDefaultTenantRole,
} from '@/ee/pages/admin/roles/default-tenant-role';

export default function AdminRoles() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [keyword, setKeyword] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [editingRole, setEditingRole] = useState<RoleTableRow>();
  const { roleList, isFetching } = useRoleList();
  const {
    defaultRoleId,
    isLoading: isDefaultRoleLoading,
    isError: isDefaultRoleError,
    isUpdating: isUpdatingDefaultRole,
    setDefaultRole,
  } = useDefaultTenantRole();
  const { createRole, isPending: isCreatingRole } = useCreateRole();

  const updateRoleMutation = useMutation({
    mutationFn: async (role: RoleTableRow) => {
      const { data } = await updateRoleDescription(
        role.role_name,
        role.description?.trim() ?? '',
      );
      if (data.code !== 0) throw new Error(data.message);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: ['admin/listRolesWithPermission'],
      });
      setEditingRole(undefined);
      message.success(t('permission.roleUpdated'));
    },
    onError: (error) => message.error(error.message),
  });
  const deleteRoleMutation = useMutation({
    mutationFn: async (role: RoleTableRow) => {
      const { data } = await deleteAdminRole(role.role_name);
      if (data.code !== 0) throw new Error(data.message);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: ['admin/listRolesWithPermission'],
      });
      message.success(t('permission.roleDeleted'));
    },
    onError: (error) => message.error(error.message),
  });

  const tableRoles: RoleTableRow[] = roleList.map((role) => ({
    id: role.id,
    role_name: role.role_name,
    description: role.description,
    is_default: defaultRoleId === role.id,
    count: role.tenant_count,
  }));

  return (
    <section className="size-full flex min-w-0 flex-col">
      <div className="flex items-center justify-between gap-4 pb-4">
        <h1 className="text-2xl font-semibold text-text-primary">
          {t('admin.roles')}
        </h1>
      </div>
      <DefaultTenantRoleLoadError isError={isDefaultRoleError} />
      <RoleTableView
        keyword={keyword}
        roles={tableRoles}
        loading={isFetching}
        canManageRoles
        settingDefault={isDefaultRoleLoading || isUpdatingDefaultRole}
        deleting={deleteRoleMutation.isPending}
        countLabel={t('admin.tenants')}
        toolbar
        onKeywordChange={setKeyword}
        onCreate={() => setCreateOpen(true)}
        onSetDefault={(roleId) => setDefaultRole(roleId)}
        onConfigure={(role) =>
          navigate(
            PrivateRoutes.AdminRoleConfiguration.replace(
              ':roleName',
              encodeURIComponent(role.role_name),
            ),
          )
        }
        onEdit={setEditingRole}
        onDelete={(role) => deleteRoleMutation.mutate(role)}
      />
      {(createOpen || editingRole) && (
        <RoleFormDialog
          key={editingRole?.id ?? 'create'}
          roleName={editingRole?.role_name}
          description={editingRole?.description}
          canEditRoleName={!editingRole}
          loading={isCreatingRole || updateRoleMutation.isPending}
          onClose={() => {
            setCreateOpen(false);
            setEditingRole(undefined);
          }}
          onSubmit={async (values) => {
            if (editingRole) {
              updateRoleMutation.mutate({
                ...editingRole,
                description: values.description,
              });
              return;
            }
            try {
              await createRole(values);
              setCreateOpen(false);
              message.success(t('permission.roleCreated'));
            } catch (error) {
              message.error(
                error instanceof Error
                  ? error.message
                  : t('message.networkAnomalyDescription'),
              );
            }
          }}
        />
      )}
    </section>
  );
}
