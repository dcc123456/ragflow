import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import { EmptyType } from '@/components/empty/constant';
import Empty from '@/components/empty/empty';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { UseRowSelectionType } from '@/hooks/logic-hooks/use-row-selection';
import { Pagination } from '@/interfaces/common';
import { IEvaluationCollection } from '@/interfaces/database/evaluation';
import {
  ColumnDef,
  ColumnFiltersState,
  SortingState,
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table';
import { Eye, PenLine, Trash2 } from 'lucide-react';
import { FC, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

type EvaluationTableProps = {
  data: IEvaluationCollection[];
  pagination: Pagination;
  setPagination: (pagination: { page: number; pageSize: number }) => void;
  loading: boolean;
  onEdit: (item: IEvaluationCollection) => void;
  onView: (item: IEvaluationCollection) => void;
  onDelete: (item: IEvaluationCollection) => void;
} & Pick<UseRowSelectionType, 'rowSelection' | 'setRowSelection'>;

export const getEvaluationTableColumns = (
  onEdit: (item: IEvaluationCollection) => void,
  onView: (item: IEvaluationCollection) => void,
  onDelete: (item: IEvaluationCollection) => void,
  t: (key: string) => string,
) => {
  const columns: ColumnDef<IEvaluationCollection>[] = [
    {
      id: 'select',
      header: ({ table }) => (
        <Checkbox
          checked={
            table.getIsAllPageRowsSelected() ||
            (table.getIsSomePageRowsSelected() && 'indeterminate')
          }
          onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
          aria-label="Select all"
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(!!value)}
          aria-label="Select row"
          disabled={!row.getCanSelect()}
        />
      ),
      meta: { cellClassName: 'w-12' },
    },
    {
      accessorKey: 'name',
      header: t('fileManager.name'),
      cell: ({ row }) => (
        <div className="text-text-primary">{row.original.name}</div>
      ),
    },
    {
      accessorKey: 'dataId',
      header: t('fileManager.evaluation.dataId'),
      cell: ({ row }) => (
        <div className="text-text-primary">{row.original.id}</div>
      ),
    },
    {
      accessorKey: 'createdAt',
      header: t('fileManager.evaluation.createdAt'),
      cell: ({ row }) => (
        <div className="text-text-primary">{row.original.create_date}</div>
      ),
    },
    {
      id: 'operations',
      header: t('common.action'),
      cell: ({ row }) => {
        const item = row.original;
        return (
          <div className="flex justify-start space-x-2 opacity-0 group-hover:opacity-100 transition-opacity">
            <Button
              variant="ghost"
              size="sm"
              className="p-1 bg-transparent"
              onClick={() => onEdit(row.original)}
              title={t('fileManager.evaluation.editName')}
            >
              <PenLine size={16} />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="p-1 bg-transparent"
              onClick={() => onView(row.original)}
              title={t('fileManager.evaluation.view')}
            >
              <Eye size={16} />
            </Button>
            <ConfirmDeleteDialog
              content={{
                title: ' ',
                node: (
                  <ConfirmDeleteDialogNode
                    name={item.name}
                    // warnText={t('common.deleteWarning')}
                  />
                ),
              }}
              onOk={() => onDelete(item)}
              okButtonText={t('common.delete')}
              cancelButtonText={t('common.cancel')}
            >
              <Button
                variant="delete"
                size="sm"
                className="p-1 bg-transparent"
                title={t('fileManager.evaluation.delete')}
              >
                <Trash2 size={16} />
              </Button>
            </ConfirmDeleteDialog>
          </div>
        );
      },
      // meta: { cellClassName: 'text-right' },
    },
  ];

  return columns;
};

export const EvaluationTable: FC<EvaluationTableProps> = ({
  data,
  pagination,
  setPagination,
  loading,
  rowSelection,
  setRowSelection,
  onEdit,
  onView,
  onDelete,
}) => {
  const { t } = useTranslation();
  const [sorting, setSorting] = useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([]);

  const handlePaginationChange = (page: number, pageSize: number) => {
    setPagination({ page, pageSize });
  };

  const columns = useMemo(() => {
    return getEvaluationTableColumns(onEdit, onView, onDelete, t);
  }, [onEdit, onView, onDelete, t]);

  const currentPagination = useMemo(
    () => ({
      pageIndex: (pagination.current || 1) - 1,
      pageSize: pagination.pageSize || 10,
    }),
    [pagination],
  );

  const table = useReactTable<IEvaluationCollection>({
    data: data || [],
    columns,
    manualPagination: true,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    onRowSelectionChange: setRowSelection,
    state: {
      sorting,
      columnFilters,
      rowSelection,
      pagination: currentPagination,
    },
    pageCount: pagination.total
      ? Math.ceil(pagination.total / pagination.pageSize)
      : 0,
  });

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="text-text-secondary">
          {t('fileManager.evaluation.loading')}
        </div>
      </div>
    );
  }

  return (
    <>
      <div className="flex-1 min-h-0 size-full">
        <Table rootClassName="max-h-full overflow-auto">
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <TableHead key={header.id}>
                    {flexRender(
                      header.column.columnDef.header,
                      header.getContext(),
                    )}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {table.getRowModel().rows.length > 0 ? (
              table.getRowModel().rows.map((row) => (
                <TableRow key={row.id} className="group">
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      // className={cell.column.columnDef.meta?.cellClassName}
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
                <TableCell colSpan={5} className="h-24 text-center">
                  <Empty
                    type={EmptyType.Data}
                    text={t('fileManager.evaluation.noEvaluationData')}
                  />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      <div className="flex items-center justify-end py-4">
        <RAGFlowPagination
          current={pagination.current}
          pageSize={pagination.pageSize}
          total={pagination.total}
          onChange={handlePaginationChange}
        />
      </div>
    </>
  );
};
