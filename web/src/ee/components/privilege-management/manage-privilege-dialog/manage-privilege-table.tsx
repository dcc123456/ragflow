'use client';

import {
  ColumnDef,
  ColumnFiltersState,
  SortingState,
  VisibilityState,
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table';
import { Crown, Trash2 } from 'lucide-react';
import * as React from 'react';

import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  PermissionResourceType,
  TeamRole,
  hasResourceOwnerPermission,
} from '@/ee/constants/team';
import { UseRowSelectionType } from '@/hooks/logic-hooks/use-row-selection';
import { IPermission } from '@/ee/interfaces/database/team';
import { useTranslation } from 'react-i18next';
import { IPrivilegeManagementInitialValues } from '@/ee/components/privilege-management/interface';
import { UserTypeLabel } from '@/ee/components/privilege-management/privilege-label';
import { PermissionCell } from '@/ee/components/privilege-management/manage-privilege-dialog/permission-cell';
import { useOperatePermission } from '@/ee/components/privilege-management/manage-privilege-dialog/use-operate-permission';

type ManagePrivilegeTableProps = Pick<
  UseRowSelectionType,
  'rowSelection' | 'setRowSelection'
> & {
  initialValues: IPrivilegeManagementInitialValues;
  data: IPermission[];
  resourceType: PermissionResourceType;
  canGrantManage: boolean;
  canTransferOwner: boolean;
  transferOwnerLoading: boolean;
  tenantMemberUserIds: ReadonlyMap<string, string>;
  onTransferOwner: (record: IPermission) => void;
};

function checkOwner(permissions: IPermission['permissions']) {
  return hasResourceOwnerPermission(permissions);
}

export function ManagePrivilegeTable({
  initialValues,
  rowSelection,
  setRowSelection,
  data,
  resourceType,
  canGrantManage,
  canTransferOwner,
  transferOwnerLoading,
  tenantMemberUserIds,
  onTransferOwner,
}: ManagePrivilegeTableProps) {
  const { t } = useTranslation();
  const [sorting, setSorting] = React.useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = React.useState<ColumnFiltersState>(
    [],
  );
  const [columnVisibility, setColumnVisibility] =
    React.useState<VisibilityState>({});

  const { handleDelete, setPermission } = useOperatePermission({
    initialValues,
  });
  const resourceIds = React.useMemo(
    () =>
      resourceType === PermissionResourceType.Document
        ? initialValues.documents || []
        : [initialValues.id],
    [initialValues.documents, initialValues.id, resourceType],
  );
  const handleChangePermission = React.useCallback(
    (record: IPermission) => (permission: number) =>
      setPermission(permission, [record]),
    [setPermission],
  );

  const columns = React.useMemo(() => {
    const columns: ColumnDef<IPermission>[] = [
      {
        id: 'select',
        header: ({ table }) => (
          <div className="flex items-center justify-center">
            <Checkbox
              checked={
                table.getIsAllPageRowsSelected() ||
                (table.getIsSomePageRowsSelected() && 'indeterminate')
              }
              onCheckedChange={(value) =>
                table.toggleAllPageRowsSelected(!!value)
              }
              aria-label="Select all"
            />
          </div>
        ),
        cell: ({ row }) =>
          checkOwner(row.original.permissions) ? null : (
            <div className="flex items-center justify-center">
              <Checkbox
                checked={row.getIsSelected()}
                onCheckedChange={(value) => row.toggleSelected(!!value)}
                aria-label="Select row"
              />
            </div>
          ),
        enableSorting: false,
        enableHiding: false,
      },
      {
        accessorKey: 'name',
        header: t('common.name'),
        cell: ({ row }) => (
          <div className="flex items-center justify-center gap-3">
            <RAGFlowAvatar
              isPerson
              className="size-7"
              name={row.original.name}
              avatar={row.original.avatar}
            />
            <div className="min-w-0 text-center">
              <div className="truncate text-sm font-medium text-text-primary">
                {row.getValue('name')}
              </div>
            </div>
          </div>
        ),
      },
      {
        accessorKey: 'permissions',
        header: t('permission.permission'),
        cell: ({ row }) => {
          return (
            <PermissionCell
              permissions={row.original.permissions}
              resourceIds={resourceIds}
              resourceType={resourceType}
              editable={!checkOwner(row.original.permissions)}
              canGrantManage={canGrantManage}
              onChange={handleChangePermission(row.original)}
            ></PermissionCell>
          );
        },
      },
      {
        accessorKey: 'role',
        header: t('permission.type'),
        cell: ({ row }) => (
          <UserTypeLabel role={row.getValue('role')}></UserTypeLabel>
        ),
      },
      {
        id: 'actions',
        enableHiding: false,
        header: () => <div>{t('common.action')}</div>,
        cell: ({ row }) => {
          if (checkOwner(row.original.permissions)) {
            return;
          }
          const canTransferOwnerToMember =
            canTransferOwner &&
            row.original.role === TeamRole.Member &&
            tenantMemberUserIds.has(row.original.id);

          return (
            <div className="flex items-center justify-center gap-1">
              {canTransferOwnerToMember && (
                <Button
                  variant="ghost"
                  className="h-8 w-8 p-0 text-accent-primary"
                  disabled={transferOwnerLoading}
                  onClick={() => void onTransferOwner(row.original)}
                  aria-label={t('permission.transferOwner')}
                  title={t('permission.transferOwner')}
                >
                  <Crown />
                </Button>
              )}
              <Button
                variant="ghost"
                className="h-8 w-8 p-0 text-state-error hover:text-state-error"
                onClick={handleDelete(row.original)}
              >
                <span className="sr-only">{t('common.delete')}</span>
                <Trash2 />
              </Button>
            </div>
          );
        },
      },
    ];

    return columns;
  }, [
    canGrantManage,
    canTransferOwner,
    handleChangePermission,
    handleDelete,
    onTransferOwner,
    resourceIds,
    resourceType,
    t,
    tenantMemberUserIds,
    transferOwnerLoading,
  ]);

  const table = useReactTable({
    data,
    columns,
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    getCoreRowModel: getCoreRowModel(),
    // getPaginationRowModel: getPaginationRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    onColumnVisibilityChange: setColumnVisibility,
    onRowSelectionChange: setRowSelection,
    state: {
      sorting,
      columnFilters,
      columnVisibility,
      rowSelection,
    },
    enableRowSelection(row) {
      return !checkOwner(row.original.permissions);
    },
  });

  return (
    <div className="w-full">
      <div className="overflow-hidden rounded-xl border bg-background shadow-sm">
        <Table rootClassName="max-h-[60vh]">
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  return (
                    <TableHead
                      key={header.id}
                      className="h-11 bg-bg-card/70 text-center text-sm font-medium text-text-secondary"
                    >
                      {header.isPlaceholder
                        ? null
                        : flexRender(
                            header.column.columnDef.header,
                            header.getContext(),
                          )}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() && 'selected'}
                  className="border-b border-border/70 transition-colors hover:bg-bg-card/40 data-[state=selected]:bg-accent-primary/5"
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      className="text-center align-middle"
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
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className="h-24 text-center text-text-secondary"
                >
                  No results.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
