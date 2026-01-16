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
import { SquarePen, Trash2 } from 'lucide-react';
import * as React from 'react';

import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Permission, PermissionResourceType } from '@/constants/team';
import { UseRowSelectionType } from '@/hooks/logic-hooks/use-row-selection';
import { IPermission } from '@/interfaces/database/team';
import { useTranslation } from 'react-i18next';
import { IPrivilegeManagementInitialValues } from '../interface';
import { PrivilegeAvatar } from '../privilege-avatar';
import { UserTypeLabel } from '../privilege-label';
import {
  getPermission,
  hideEditPermissionDropdownItem,
  hidePermissionDropdownButton,
} from '../utils';
import { PermissionCell } from './permission-cell';
import { useOperatePermission } from './use-operate-permission';

type ManagePrivilegeTableProps = Pick<
  UseRowSelectionType,
  'rowSelection' | 'setRowSelection'
> & {
  initialValues: IPrivilegeManagementInitialValues;
  data: IPermission[];
  resourceType: PermissionResourceType;
};

function checkOwner(permissions: IPermission['permissions']) {
  return getPermission(permissions) === Permission.Owner;
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

  const { handleDelete, handleSwitchPermission } = useOperatePermission({
    initialValues,
  });

  const columns = React.useMemo(() => {
    const columns: ColumnDef<IPermission>[] = [
      {
        id: 'select',
        header: ({ table }) => (
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
        ),
        cell: ({ row }) =>
          checkOwner(row.original.permissions) ? null : (
            <Checkbox
              checked={row.getIsSelected()}
              onCheckedChange={(value) => row.toggleSelected(!!value)}
              aria-label="Select row"
            />
          ),
        enableSorting: false,
        enableHiding: false,
      },
      {
        accessorKey: 'name',
        header: t('common.name'),
        cell: ({ row }) => (
          <div className="flex gap-2 items-center">
            <PrivilegeAvatar></PrivilegeAvatar> {row.getValue('name')}
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
          const onUpdatePermission = (value: string) => {
            handleSwitchPermission(value, row.original);
          };
          return (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" className="h-8 w-8 p-0">
                  <span className="sr-only">Open menu</span>
                  <SquarePen />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <RadioGroup
                  value={row.original.permissions?.toString()}
                  onValueChange={onUpdatePermission}
                >
                  {hidePermissionDropdownButton(resourceType) || (
                    <>
                      <DropdownMenuItem>
                        <div className="flex items-center space-x-2">
                          <RadioGroupItem value="1" id="1" />
                          <Label htmlFor="1">
                            {t('permission.readPermission')}
                          </Label>
                        </div>
                      </DropdownMenuItem>
                      {hideEditPermissionDropdownItem(resourceType) || (
                        <DropdownMenuItem>
                          <div className="flex items-center space-x-2">
                            <RadioGroupItem value="2" id="2" />
                            <Label htmlFor="2">
                              {t('permission.writePermission')}
                            </Label>
                          </div>
                        </DropdownMenuItem>
                      )}
                      <DropdownMenuItem>
                        <div className="flex items-center space-x-2">
                          <RadioGroupItem value="4" id="4" />
                          <Label htmlFor="4">
                            {t('permission.managePermission')}
                          </Label>
                        </div>
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                    </>
                  )}
                  <DropdownMenuItem onClick={handleDelete(row.original)}>
                    <Trash2 /> {t('common.delete')}
                  </DropdownMenuItem>
                </RadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          );
        },
      },
    ];

    return columns;
  }, [handleDelete, handleSwitchPermission, resourceType, t]);

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
      <div className="rounded-md border">
        <Table rootClassName="max-h-[60vh]">
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  return (
                    <TableHead key={header.id}>
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
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
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
                  className="h-24 text-center"
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
