import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { SelectWithSearch } from '@/components/originui/select-with-search';
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
import { TenantRole } from '@/ee/constants/team';
import { ITenantRole } from '@/ee/interfaces/database/team';
import {
  useDeleteTenantUser,
  useFetchTenantInfo,
  useListTenantUser,
} from '@/hooks/use-user-setting-request';
import { ITenantUser } from '@/interfaces/database/user-setting';
import { TenantMembershipStatus } from '@/pages/user-setting/constants';
import { formatDate } from '@/utils/date';
import {
  ColumnDef,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  SortingState,
  useReactTable,
} from '@tanstack/react-table';
import { ArrowUpDown, Trash2 } from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useSetTenantMemberRole,
  useTenantMyPermissions,
  useTenantRoles,
} from './use-member-role-query';

type MemberTableProps = {
  keyword: string;
};

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
  const [pendingRoleId, setPendingRoleId] = useState<string>();

  const roleLabel =
    member.role === TenantRole.Owner
      ? t('permission.owner')
      : member.status === TenantMembershipStatus.Invited
        ? t('permission.pending')
        : roleNameMap.get(member.role) || member.role;

  const currentRoleId = roleNameMap.has(member.role) ? member.role : undefined;

  useEffect(() => {
    setPendingRoleId(undefined);
  }, [member.role]);

  const handleRoleChange = useCallback(
    async (value: string) => {
      setPendingRoleId(value);

      const code = await setMemberRole(Number(value));

      if (code !== 0) {
        setPendingRoleId(undefined);
      }
    },
    [setMemberRole],
  );

  if (!canEditRole || roles.length === 0) {
    return <div>{roleLabel}</div>;
  }

  return (
    <SelectWithSearch
      value={pendingRoleId ?? currentRoleId ?? ''}
      onChange={handleRoleChange}
      options={roles.map((role) => ({
        label: role.role_name,
        value: String(role.id),
      }))}
      placeholder={roleLabel}
      disabled={saving}
      triggerClassName="h-8 w-[140px] !bg-transparent px-2 text-text-primary"
    />
  );
}

function MemberActionCell({
  member,
  canDelete,
}: {
  member: ITenantUser;
  canDelete: boolean;
}) {
  const { t } = useTranslation();
  const { deleteTenantUser } = useDeleteTenantUser();

  const handleDelete = useCallback(async () => {
    await deleteTenantUser({ userId: member.user_id });
  }, [deleteTenantUser, member.user_id]);

  if (!canDelete) {
    return null;
  }

  return (
    <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 transition-opacity">
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
    </div>
  );
}

export function MemberTable({ keyword }: MemberTableProps) {
  const { t } = useTranslation();
  const [sorting, setSorting] = useState<SortingState>([]);
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo.tenant_id;
  const { data: members, loading } = useListTenantUser(tenantId);
  const { data: myPermissions } = useTenantMyPermissions(tenantId);
  const { data: roles } = useTenantRoles(tenantId);

  const canManageMembers = !!myPermissions?.tenant_permissions.manage_members;
  const canManageRoles = !!myPermissions?.tenant_permissions.manage_roles;

  const roleNameMap = useMemo(() => {
    return new Map(roles.map((role) => [String(role.id), role.role_name]));
  }, [roles]);

  const data = useMemo(() => {
    const normalizedKeyword = keyword.trim().toLowerCase();

    if (!normalizedKeyword) {
      return members;
    }

    return members.filter((member) => {
      return (
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
              canDelete={canManageMembers && !isOwner}
            />
          );
        },
      },
    ];

    return columns;
  }, [canManageMembers, canManageRoles, roleNameMap, roles, t, tenantId]);

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
            table.getRowModel().rows.map((row) => (
              <TableRow key={row.id} className="group">
                {row.getVisibleCells().map((cell) => (
                  <TableCell
                    key={cell.id}
                    className={cell.column.columnDef.meta?.cellClassName}
                  >
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </TableCell>
                ))}
              </TableRow>
            ))
          ) : (
            <TableEmpty columnsLength={columns.length} />
          )}
        </TableBody>
      </Table>
    </div>
  );
}
