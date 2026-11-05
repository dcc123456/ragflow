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
import {
  Check,
  Eye,
  Minus,
  ShieldCheck,
  SquarePen,
  Trash2,
} from 'lucide-react';
import * as React from 'react';

import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
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
import {
  Permission,
  PermissionResourceType,
  hasResourceOwnerPermission,
  hasResourcePermission,
} from '@/ee/constants/team';
import { UseRowSelectionType } from '@/hooks/logic-hooks/use-row-selection';
import { IPermission } from '@/ee/interfaces/database/team';
import { cn } from '@/lib/utils';
import { useTranslation } from 'react-i18next';
import { IPrivilegeManagementInitialValues } from '@/ee/components/privilege-management/interface';
import { PrivilegeAvatar } from '@/ee/components/privilege-management/privilege-avatar';
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
}: ManagePrivilegeTableProps) {
  const { t } = useTranslation();
  const [sorting, setSorting] = React.useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = React.useState<ColumnFiltersState>(
    [],
  );
  const [columnVisibility, setColumnVisibility] =
    React.useState<VisibilityState>({});

  const { handleDelete, togglePermission } = useOperatePermission({
    initialValues,
  });

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
            <PrivilegeAvatar className="size-8 ring-1 ring-black/5" />
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
          const permissions: Record<string, number> =
            row.getValue('permissions');
          return (
            <PermissionCell
              permissions={permissions}
              resourceType={resourceType}
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
          const resourceIds =
            resourceType === PermissionResourceType.Document
              ? initialValues.documents || []
              : [initialValues.id];
          const onTogglePermission = (permission: Permission) =>
            togglePermission(permission, [row.original]);
          return (
            <div className="flex items-center justify-center gap-1">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" className="h-8 w-8 p-0">
                    <span className="sr-only">Open menu</span>
                    <SquarePen />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="min-w-48 p-1.5">
                  {(
                    [
                      [Permission.Read, Eye, 'readPermission'],
                      [Permission.Write, SquarePen, 'writePermission'],
                      [Permission.Manage, ShieldCheck, 'managePermission'],
                    ] as const
                  ).map(([permission, Icon, label]) => {
                    const selectedCount = resourceIds.filter((resourceId) =>
                      hasResourcePermission(
                        row.original.permissions[resourceId] ?? 0,
                        permission,
                      ),
                    ).length;
                    const checked =
                      resourceIds.length > 0 &&
                      selectedCount === resourceIds.length;
                    const partiallyChecked = selectedCount > 0 && !checked;
                    return (
                      <DropdownMenuItem
                        key={permission}
                        role="menuitemcheckbox"
                        aria-checked={partiallyChecked ? 'mixed' : checked}
                        className={cn(
                          'rounded-md py-2 text-sm',
                          checked && 'bg-bg-card text-text-primary',
                        )}
                        onClick={() => onTogglePermission(permission)}
                      >
                        <Icon className="mr-2 size-4" />
                        {t(`permission.${label}`)}
                        {checked && <Check className="ml-auto size-4" />}
                        {partiallyChecked && (
                          <Minus className="ml-auto size-4" />
                        )}
                      </DropdownMenuItem>
                    );
                  })}
                </DropdownMenuContent>
              </DropdownMenu>
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
  }, [handleDelete, initialValues, resourceType, t, togglePermission]);

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
