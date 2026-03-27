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
import { ArrowUpDown, ChevronRight } from 'lucide-react';
import * as React from 'react';

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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import {
  useFetchDepartmentList,
  useFetchDepartmentMemberList,
} from '@/hooks/use-team';
import { IDepartment, IMember } from '@/interfaces/database/team';
import { cn } from '@/lib/utils';
import { useEffect, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { CellName } from '../cell-name';
import {
  useModifyDepartment,
  useShowMoveDepartmentDialog,
} from '../use-operate-department';
import { useTenantId } from '../use-operate-team';
import { ActionCell } from './action-cell';
import { useSwitchBreadcrumb } from './use-switch-breadcrumb';

type DepartmentTableType = Omit<
  ReturnType<typeof useSwitchBreadcrumb>,
  'switchToHomeBreadcrumb'
> &
  Pick<ReturnType<typeof useModifyDepartment>, 'showDepartmentModal'> & {
    departmentParentId?: string;
  } & Pick<
    ReturnType<typeof useShowMoveDepartmentDialog>,
    'showMoveDepartmentModal'
  > & {
    departmentParentId?: string;
  };

export function DepartmentTable({
  breadcrumbs,
  setBreadcrumbs,
  showDepartmentModal,
  departmentParentId,
  showMoveDepartmentModal,
}: DepartmentTableType) {
  const parentDepartmentId = useMemo(() => {
    const latestBreadcrumb = breadcrumbs.at(-1);
    return latestBreadcrumb?.value;
  }, [breadcrumbs]);

  const [sorting, setSorting] = React.useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = React.useState<ColumnFiltersState>(
    [],
  );
  const [columnVisibility, setColumnVisibility] =
    React.useState<VisibilityState>({});
  const [rowSelection, setRowSelection] = React.useState({});
  const { t } = useTranslation('translation', {
    keyPrefix: 'fileManager',
  });
  const tenantId = useTenantId();
  const { data, loading, setFetchDepartmentListParams } =
    useFetchDepartmentList(tenantId);
  const {
    data: departmentMemberList,
    loading: departmentMemberListLoading,
    setId,
    setTeamId,
  } = useFetchDepartmentMemberList();

  const nextList = useMemo(() => {
    return [...data, ...departmentMemberList];
  }, [data, departmentMemberList]);

  useEffect(() => {
    setId(departmentParentId ? departmentParentId : '');
  }, [departmentParentId, setId]);

  useEffect(() => {
    setFetchDepartmentListParams({ parentId: parentDepartmentId || '' });
  }, [parentDepartmentId, setFetchDepartmentListParams]);

  useEffect(() => {
    setId(''); // Switch team, switch to the root department, and prohibit requesting sub-department members
    setTeamId(tenantId);
  }, [setId, setTeamId, tenantId]);

  const columns = useMemo(() => {
    const columns: ColumnDef<IDepartment | IMember>[] = [
      {
        accessorKey: 'name',
        header: ({ column }) => {
          return (
            <Button
              variant="ghost"
              onClick={() =>
                column.toggleSorting(column.getIsSorted() === 'asc')
              }
            >
              {t('name')}
              <ArrowUpDown />
            </Button>
          );
        },
        meta: { cellClassName: 'max-w-[20vw]' },
        cell: ({ row }) => {
          const record = row.original;
          const name: string =
            row.getValue('name') || (record as IMember).nickname;
          const isDepartment = 'department_id' in record;

          const handleNameClick = () => {
            if (!isDepartment) {
              return;
            }
            setBreadcrumbs((pre) => {
              return [
                ...pre,
                {
                  label: row.getValue('name'),
                  value: (record as IDepartment).department_id,
                },
              ];
            });
          };

          return (
            <Tooltip>
              <TooltipTrigger asChild>
                <div
                  className={cn('flex gap-2 items-center', {
                    ['cursor-pointer']: isDepartment,
                  })}
                  onClick={handleNameClick}
                >
                  <CellName name={name} avatar={record.avatar}></CellName>
                  {isDepartment && <ChevronRight className="size-4" />}
                </div>
              </TooltipTrigger>
              <TooltipContent>
                <p>{name}</p>
              </TooltipContent>
            </Tooltip>
          );
        },
      },

      {
        id: 'actions',
        header: t('action'),
        enableHiding: false,
        cell: ({ row }) => {
          return (
            <ActionCell
              row={row}
              showDepartmentModal={showDepartmentModal}
              showMoveDepartmentModal={showMoveDepartmentModal}
              parentDepartmentId={parentDepartmentId}
            ></ActionCell>
          );
        },
      },
    ];

    return columns;
  }, [
    parentDepartmentId,
    setBreadcrumbs,
    showDepartmentModal,
    showMoveDepartmentModal,
    t,
  ]);

  const table = useReactTable({
    data: nextList || [],
    columns,
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    getCoreRowModel: getCoreRowModel(),
    // getPaginationRowModel: getPaginationRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    onColumnVisibilityChange: setColumnVisibility,
    onRowSelectionChange: setRowSelection,

    manualPagination: true, //we're doing manual "server-side" pagination

    state: {
      sorting,
      columnFilters,
      columnVisibility,
      rowSelection,
    },
    debugTable: true,
  });

  return (
    <div className="w-full space-y-4">
      <div className="rounded-md border">
        <Table rootClassName="max-h-[76vh]">
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
            {loading || departmentMemberListLoading ? (
              <TableSkeleton columnsLength={columns.length}></TableSkeleton>
            ) : table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() && 'selected'}
                  className="group"
                >
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
              <TableEmpty columnsLength={columns.length}></TableEmpty>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
