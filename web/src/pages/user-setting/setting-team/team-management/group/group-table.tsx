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
import { ArrowUpDown } from 'lucide-react';
import * as React from 'react';

import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { TableEmpty, TableSkeleton } from '@/components/table-skeleton';
import { AvatarGroup } from '@/components/ui/avatar';
import { Button } from '@/components/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { TenantIdContext } from '@/contexts/teant-context';
import { useFetchGroupList } from '@/hooks/use-team';
import { useFetchUserInfo } from '@/hooks/use-user-setting-request';
import { IGroup, IMember } from '@/interfaces/database/team';
import { useContext, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { CellNameWithToolTip } from '../cell-name';
import { useModifyGroupMember, useTransferOwner } from '../use-operate-group';
import { ActionCell } from './action-cell';

type GroupTableProps = Pick<
  ReturnType<typeof useModifyGroupMember>,
  'showGroupMemberModal'
> &
  Pick<ReturnType<typeof useTransferOwner>, 'showTransferOwnerModal'>;

export function GroupTable({
  showGroupMemberModal,
  showTransferOwnerModal,
}: GroupTableProps) {
  const [sorting, setSorting] = React.useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = React.useState<ColumnFiltersState>(
    [],
  );
  const [columnVisibility, setColumnVisibility] =
    React.useState<VisibilityState>({});
  const [rowSelection, setRowSelection] = React.useState({});
  const { t } = useTranslation();
  const tenantId = useContext(TenantIdContext);

  const { data: userInfo } = useFetchUserInfo();

  const { data, loading } = useFetchGroupList(tenantId);

  const columns = useMemo(() => {
    const columns: ColumnDef<IGroup>[] = [
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
              {t('fileManager.name')}
              <ArrowUpDown />
            </Button>
          );
        },
        meta: { cellClassName: 'max-w-[20vw]' },
        cell: ({ row }) => {
          const name: string = row.getValue('name');

          return (
            <CellNameWithToolTip
              name={name}
              avatar={row.original.avatar}
            ></CellNameWithToolTip>
          );
        },
      },
      {
        accessorKey: 'owner_name',
        header: ({ column }) => {
          return (
            <Button
              variant="ghost"
              onClick={() =>
                column.toggleSorting(column.getIsSorted() === 'asc')
              }
            >
              {t('permission.owner')}
              <ArrowUpDown />
            </Button>
          );
        },
        cell: ({ row }) => <div>{row.getValue('owner_name')}</div>,
      },
      {
        accessorKey: 'members',
        header: ({ column }) => {
          return (
            <Button
              variant="ghost"
              onClick={() =>
                column.toggleSorting(column.getIsSorted() === 'asc')
              }
            >
              {t('permission.member')}
              <ArrowUpDown />
            </Button>
          );
        },
        cell: ({ row }) => {
          const members: IMember[] = row.getValue('members');

          return (
            <AvatarGroup className="flex-row">
              {members.map((x) => (
                <RAGFlowAvatar
                  name={x.nickname}
                  avatar={x.avatar}
                  isPerson
                  className="size-5"
                  key={x.member_id}
                />
              ))}
            </AvatarGroup>
          );
        },
      },
      {
        id: 'actions',
        header: t('fileManager.action'),
        enableHiding: false,
        cell: ({ row }) => {
          return (
            <ActionCell
              row={row}
              showGroupMemberModal={showGroupMemberModal}
              showTransferOwnerModal={showTransferOwnerModal}
              userId={userInfo.id}
            ></ActionCell>
          );
        },
      },
    ];

    return columns;
  }, [showGroupMemberModal, showTransferOwnerModal, t, userInfo.id]);

  const table = useReactTable({
    data: data,
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
    debugTable: true,
  });

  return (
    <div className="w-full">
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
            {loading ? (
              <TableSkeleton columnsLength={columns.length}></TableSkeleton>
            ) : table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() && 'selected'}
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
