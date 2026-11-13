import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { TableEmpty, TableSkeleton } from '@/components/table-skeleton';
import message from '@/components/ui/message';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { TenantRole } from '@/ee/constants/team';
import { useSwitchTenant } from '@/ee/hooks/use-switch-tenant';
import {
  ITenantManagementGrant,
  ITenantRole,
} from '@/ee/interfaces/database/team';
import { useLogout } from '@/hooks/use-login-request';
import {
  useDeleteTenantUser,
  useFetchTenantInfo,
  useFetchUserInfo,
  useListTenant,
  useListTenantUser,
} from '@/hooks/use-user-setting-request';
import { ITenantUser } from '@/interfaces/database/user-setting';
import { TenantMembershipStatus } from '@/pages/user-setting/constants';
import { deleteTenantUser } from '@/services/user-service';
import { formatDate } from '@/utils/date';
import {
  ColumnDef,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  SortingState,
  useReactTable,
} from '@tanstack/react-table';
import { useMutation } from '@tanstack/react-query';
import { ArrowUpDown, Check, LogOut, ShieldCheck, Trash2 } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useApproveTenantMember,
  useSetTenantMemberRole,
  useTenantMyPermissions,
  useTenantRoles,
} from './use-member-role-query';
import { TenantMemberPermissionsDialog } from './tenant-member-permissions-dialog';
import { useTenantManagementPermissions } from './use-tenant-member-permissions';

type MemberTableProps = {
  keyword: string;
};

const RoleTriggerClassName =
  'h-8 w-fit max-w-[140px] justify-start px-2 truncate';
const TenantMembershipRequestedStatus = '4';

function MemberRoleCell({
  tenantId,
  member,
  canEditRole,
  roles,
  roleNameMap,
}: {
  tenantId: string;
  member: ITenantUser;
  canEditRole: boolean;
  roles: ITenantRole[];
  roleNameMap: Map<string, string>;
}) {
  const { t } = useTranslation();
  const { loading: saving, setMemberRole } = useSetTenantMemberRole(
    tenantId,
    member.user_id,
  );

  const roleLabel =
    member.role === TenantRole.Owner
      ? t('permission.owner')
      : member.status === TenantMembershipStatus.Left
        ? t('permission.left')
        : member.status === TenantMembershipStatus.Invited
          ? t('permission.inviting')
          : member.status === TenantMembershipRequestedStatus
            ? t('permission.pendingReview')
            : roleNameMap.get(member.role) || member.role;

  const currentRoleId = roleNameMap.has(member.role) ? member.role : undefined;
  const displayedRole =
    member.status === TenantMembershipStatus.Left
      ? roleLabel
      : currentRoleId
        ? roleNameMap.get(currentRoleId) || roleLabel
        : roleLabel;

  const handleRoleChange = useCallback(
    (roleId: number) => () => void setMemberRole(roleId),
    [setMemberRole],
  );

  if (!canEditRole || roles.length === 0) {
    return (
      <Button
        variant="ghost"
        className={RoleTriggerClassName}
        disabled
        aria-label={t('permission.role')}
      >
        {displayedRole}
      </Button>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className={RoleTriggerClassName}
          disabled={saving}
          aria-label={t('permission.role')}
        >
          {displayedRole}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        {roles.map((role) => (
          <DropdownMenuItem key={role.id} onClick={handleRoleChange(role.id)}>
            {role.role_name}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function LeaveTenantAction({
  member,
  tenantId,
}: {
  member: ITenantUser;
  tenantId: string;
}) {
  const { t } = useTranslation();
  const { data: tenants, loading: loadingTenants } = useListTenant();
  const { switchToTenant } = useSwitchTenant();
  const { logout } = useLogout();
  const { mutate: leaveTenant, isPending } = useMutation({
    mutationKey: ['leaveTenant', tenantId, member.user_id],
    mutationFn: async () => {
      const { data } = await deleteTenantUser({
        tenantId,
        userId: member.user_id,
      });

      return data?.code;
    },
    onSuccess: async (code) => {
      if (code !== 0) {
        return;
      }

      message.success(t('message.operated'));
      const nextTenant = tenants.find(
        (tenant) =>
          tenant.tenant_id !== tenantId &&
          tenant.status === TenantMembershipStatus.Joined,
      );

      if (nextTenant) {
        switchToTenant(nextTenant.tenant_id);
      } else {
        await logout();
      }
    },
  });

  return (
    <ConfirmDeleteDialog
      onOk={() => leaveTenant()}
      title={t('permission.leaveTheTeam')}
      okButtonText={t('permission.leaveTheTeam')}
      content={{
        node: (
          <ConfirmDeleteDialogNode
            avatar={{
              avatar: member.avatar,
              name: member.nickname,
              isPerson: true,
            }}
            name={member.nickname}
            warnText={t('permission.leaveTenantHint')}
          />
        ),
      }}
    >
      <Button
        variant="ghost"
        size="icon-sm"
        disabled={isPending || loadingTenants}
        aria-label={t('permission.leaveTheTeam')}
        title={t('permission.leaveTheTeam')}
      >
        <LogOut />
      </Button>
    </ConfirmDeleteDialog>
  );
}

function MemberActionCell({
  member,
  tenantId,
  canDelete,
  canApprove,
  canLeave,
  canManageTenantPermissions,
  tenantPermissionsLoading,
  onManageTenantPermissions,
}: {
  member: ITenantUser;
  tenantId: string;
  canDelete: boolean;
  canApprove: boolean;
  canLeave: boolean;
  canManageTenantPermissions: boolean;
  tenantPermissionsLoading: boolean;
  onManageTenantPermissions: () => void;
}) {
  const { t } = useTranslation();
  const { deleteTenantUser } = useDeleteTenantUser();
  const { loading: approving, approveMember } = useApproveTenantMember(
    tenantId,
    member.user_id,
  );

  const handleApprove = useCallback(() => {
    void approveMember();
  }, [approveMember]);

  const handleDelete = useCallback(async () => {
    await deleteTenantUser({ userId: member.user_id });
  }, [deleteTenantUser, member.user_id]);

  if (!canDelete && !canApprove && !canLeave && !canManageTenantPermissions) {
    return null;
  }

  return (
    <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 transition-opacity">
      {canLeave && <LeaveTenantAction member={member} tenantId={tenantId} />}
      {canApprove && (
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-state-success"
          disabled={approving}
          aria-label={t('permission.approveMember')}
          title={t('permission.approveMember')}
          onClick={handleApprove}
        >
          <Check />
        </Button>
      )}
      {canManageTenantPermissions && (
        <Button
          variant="ghost"
          size="icon-sm"
          disabled={tenantPermissionsLoading}
          aria-label={t('permission.manageTenantPermissions')}
          title={t('permission.manageTenantPermissions')}
          onClick={onManageTenantPermissions}
        >
          <ShieldCheck />
        </Button>
      )}
      {canDelete && (
        <ConfirmDeleteDialog
          onOk={handleDelete}
          title={t('deleteModal.delMember')}
          content={{
            node: (
              <ConfirmDeleteDialogNode
                avatar={{
                  avatar: member.avatar,
                  name: member.nickname,
                  isPerson: true,
                }}
                name={member.nickname}
              />
            ),
          }}
        >
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t('common.delete')}
            title={t('common.delete')}
          >
            <Trash2 />
          </Button>
        </ConfirmDeleteDialog>
      )}
    </div>
  );
}

export function MemberTable({ keyword }: MemberTableProps) {
  const { t } = useTranslation();
  const [sorting, setSorting] = useState<SortingState>([]);
  const [editingPermissionsMember, setEditingPermissionsMember] =
    useState<ITenantUser>();
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data: userInfo } = useFetchUserInfo();
  const tenantId = tenantInfo.tenant_id;
  const { data: members, loading } = useListTenantUser(tenantId);
  const { data: myPermissions } = useTenantMyPermissions(tenantId);
  const { data: roles } = useTenantRoles(tenantId);

  const canManageMembers = !!myPermissions?.tenant_permissions.manage_members;
  const canManageRoles = !!myPermissions?.tenant_permissions.manage_roles;
  const canManageTenantPermissions = !!myPermissions?.tenant_permissions.owner;
  const {
    data: tenantManagementGrants,
    loading: loadingTenantManagementPermissions,
  } = useTenantManagementPermissions(tenantId, canManageTenantPermissions);

  const tenantManagementPermissionsByMember = useMemo(() => {
    const permissions = new Map<
      string,
      ITenantManagementGrant['permissions']
    >();
    tenantManagementGrants.forEach((grant) => {
      if (grant.subject_type === 'user' && grant.user_id) {
        permissions.set(grant.user_id, grant.permissions);
      }
    });
    return permissions;
  }, [tenantManagementGrants]);

  const roleNameMap = useMemo(() => {
    return new Map(roles.map((role) => [String(role.id), role.role_name]));
  }, [roles]);

  const data = useMemo(() => {
    const normalizedKeyword = keyword.trim().toLowerCase();
    return members.filter((member) => {
      return (
        !normalizedKeyword ||
        member.nickname?.toLowerCase().includes(normalizedKeyword) ||
        member.email?.toLowerCase().includes(normalizedKeyword)
      );
    });
  }, [keyword, members]);

  const columns = useMemo(() => {
    const columns: ColumnDef<ITenantUser>[] = [
      {
        accessorKey: 'nickname',
        header: t('common.name'),
        meta: { cellClassName: 'max-w-[20vw]' },
        cell: ({ row }) => (
          <div className="flex items-center gap-2 min-w-0">
            <RAGFlowAvatar
              isPerson
              className="size-7"
              name={row.original.nickname}
              avatar={row.original.avatar}
            />
            <span className="truncate">{row.original.nickname}</span>
          </div>
        ),
      },
      {
        accessorKey: 'update_date',
        sortingFn: 'datetime',
        header: ({ column }) => {
          const handleToggleSorting = () => {
            column.toggleSorting(column.getIsSorted() === 'asc');
          };

          return (
            <Button variant="ghost" onClick={handleToggleSorting}>
              {t('setting.updateDate')}
              <ArrowUpDown />
            </Button>
          );
        },
        cell: ({ row }) => <div>{formatDate(row.getValue('update_date'))}</div>,
      },
      {
        accessorKey: 'email',
        header: t('setting.email'),
        cell: ({ row }) => <div>{row.getValue('email')}</div>,
      },
      {
        accessorKey: 'role',
        header: t('permission.role'),
        cell: ({ row }) => {
          const member = row.original;
          const isOwner = member.role === TenantRole.Owner;

          return (
            <MemberRoleCell
              tenantId={tenantId}
              member={member}
              canEditRole={
                canManageRoles &&
                !isOwner &&
                member.status === TenantMembershipStatus.Joined
              }
              roles={roles}
              roleNameMap={roleNameMap}
            />
          );
        },
      },
      {
        id: 'actions',
        header: t('common.action'),
        meta: { cellClassName: 'w-[96px]' },
        cell: ({ row }) => {
          const member = row.original;
          const isOwner = member.role === TenantRole.Owner;

          return (
            <MemberActionCell
              member={member}
              tenantId={tenantId}
              canDelete={
                canManageMembers &&
                !isOwner &&
                member.status !== TenantMembershipStatus.Left &&
                member.user_id !== userInfo.id
              }
              canApprove={
                canManageMembers &&
                member.status === TenantMembershipRequestedStatus
              }
              canLeave={
                member.user_id === userInfo.id &&
                member.status === TenantMembershipStatus.Joined &&
                !isOwner
              }
              canManageTenantPermissions={
                canManageTenantPermissions &&
                !isOwner &&
                member.user_id !== userInfo.id
              }
              tenantPermissionsLoading={loadingTenantManagementPermissions}
              onManageTenantPermissions={() =>
                setEditingPermissionsMember(member)
              }
            />
          );
        },
      },
    ];

    return columns;
  }, [
    canManageMembers,
    canManageRoles,
    canManageTenantPermissions,
    loadingTenantManagementPermissions,
    roleNameMap,
    roles,
    t,
    tenantId,
    userInfo.id,
  ]);

  const table = useReactTable({
    data,
    columns,
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    state: {
      sorting,
    },
  });

  return (
    <div className="w-full">
      <Table rootClassName="max-h-[76vh]">
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => (
                <TableHead key={header.id}>
                  {header.isPlaceholder
                    ? null
                    : flexRender(
                        header.column.columnDef.header,
                        header.getContext(),
                      )}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {loading ? (
            <TableSkeleton columnsLength={columns.length} />
          ) : table.getRowModel().rows?.length ? (
            [...table.getRowModel().rows]
              .sort((left, right) => {
                const priority = (member: ITenantUser) =>
                  member.user_id === userInfo.id
                    ? 0
                    : member.role === TenantRole.Owner
                      ? 1
                      : member.status === TenantMembershipStatus.Left
                        ? 3
                        : 2;

                return priority(left.original) - priority(right.original);
              })
              .map((row) => (
                <TableRow key={row.id} className="group">
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      className={cell.column.columnDef.meta?.cellClassName}
                    >
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext(),
                      )}
                    </TableCell>
                  ))}
                </TableRow>
              ))
          ) : (
            <TableEmpty columnsLength={columns.length} />
          )}
        </TableBody>
      </Table>
      {editingPermissionsMember && (
        <TenantMemberPermissionsDialog
          tenantId={tenantId}
          member={editingPermissionsMember}
          permissions={
            tenantManagementPermissionsByMember.get(
              editingPermissionsMember.user_id,
            ) ?? []
          }
          onClose={() => setEditingPermissionsMember(undefined)}
        />
      )}
    </div>
  );
}
