import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table';

import Spotlight from '@/components/spotlight';
import { TableEmpty, TableSkeleton } from '@/components/table-skeleton';
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import { ScrollArea } from '@/components/ui/scroll-area';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { IngestionTaskStatus } from '@/constants/knowledge';
import { useSetModalState } from '@/hooks/common-hooks';

import {
  getIngestionTaskColumns,
  type IngestionTaskSortColumn,
} from './columns';
import { useIngestionTasks } from './hooks';
import { IngestionLogModal } from './log-modal';

type StatusFilter = 'all' | IngestionTaskStatus;

const StatusOptions: IngestionTaskStatus[] = [
  IngestionTaskStatus.CREATED,
  IngestionTaskStatus.SCHEDULED,
  IngestionTaskStatus.RUNNING,
  IngestionTaskStatus.STOPPING,
  IngestionTaskStatus.COMPLETED,
  IngestionTaskStatus.FAILED,
  IngestionTaskStatus.STOPPED,
];

export default function AdminIngestionTasks() {
  const { t } = useTranslation();
  const { showModal, hideModal, visible } = useSetModalState();
  const [activeLogId, setActiveLogId] = useState('');

  const [status, setStatus] = useState<StatusFilter>('all');
  const [sortColumn, setSortColumn] =
    useState<IngestionTaskSortColumn>('create_time');
  const [sortDesc, setSortDesc] = useState(true);
  const [pagination, setPagination] = useState({ page: 1, pageSize: 20 });

  const handleStatusChange = (value: string) => {
    setStatus(value as StatusFilter);
    setPagination((current) => ({ ...current, page: 1 }));
  };
  const handleSort = useCallback(
    (column: IngestionTaskSortColumn) => {
      if (column === sortColumn) {
        setSortDesc(!sortDesc);
      } else {
        setSortColumn(column);
        setSortDesc(true);
      }
      setPagination((current) => ({ ...current, page: 1 }));
    },
    [sortColumn, sortDesc],
  );
  const handlePageChange = (page: number, pageSize: number) => {
    setPagination({ page, pageSize });
  };

  const params = useMemo<AdminServiceEe.IngestionTaskListParams>(
    () => ({
      status:
        status === 'all'
          ? undefined
          : (status as unknown as AdminServiceEe.IngestionTaskStatusName),
      order_by: sortColumn,
      desc: sortDesc,
      page: pagination.page,
      page_size: pagination.pageSize,
    }),
    [status, sortColumn, sortDesc, pagination.page, pagination.pageSize],
  );

  const { data, isPending, isError, error } = useIngestionTasks(params);
  const tasks = data?.items ?? [];

  const handleShowLog = useCallback(
    (logId: string | null) => {
      if (!logId) {
        return;
      }
      setActiveLogId(logId);
      showModal();
    },
    [showModal],
  );

  const columns = useMemo(
    () =>
      getIngestionTaskColumns({
        t,
        sortColumn,
        sortDesc,
        onSort: handleSort,
        onShowLog: handleShowLog,
      }),
    [t, sortColumn, sortDesc, handleSort, handleShowLog],
  );

  const table = useReactTable({
    data: tasks,
    columns,
    getRowId: (row) => row.id,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    rowCount: data?.total ?? 0,
    state: {
      pagination: {
        pageIndex: pagination.page - 1,
        pageSize: pagination.pageSize,
      },
    },
  });

  return (
    <Card className="!shadow-none relative h-full bg-transparent overflow-hidden">
      <Spotlight />
      <ScrollArea className="size-full">
        <CardHeader className="flex flex-row items-center justify-between space-y-0">
          <CardTitle>{t('admin.ingestionTasks.title')}</CardTitle>

          <Select value={status} onValueChange={handleStatusChange}>
            <SelectTrigger className="h-10 w-48 bg-bg-input border-border-button">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">
                {t('admin.ingestionTasks.allStatuses')}
              </SelectItem>
              {StatusOptions.map((option) => (
                <SelectItem key={option} value={option}>
                  {t(`admin.ingestionTasks.status.${option}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </CardHeader>

        <CardContent>
          <Table className="table-fixed min-w-[78rem]">
            <colgroup>
              {table.getVisibleLeafColumns().map((column) => (
                <col
                  key={column.id}
                  className={column.columnDef.meta?.colClassName}
                />
              ))}
            </colgroup>

            <TableHeader>
              {table.getHeaderGroups().map((headerGroup) => (
                <TableRow key={headerGroup.id}>
                  {headerGroup.headers.map((header) => (
                    <TableHead
                      key={header.id}
                      className={header.column.columnDef.meta?.headerClassName}
                    >
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
              {isPending ? (
                <TableSkeleton columnsLength={columns.length} />
              ) : isError ? (
                <TableRow>
                  <TableCell
                    colSpan={columns.length}
                    className="h-24 text-center text-text-secondary"
                  >
                    {error instanceof Error
                      ? error.message
                      : t('admin.ingestionTasks.loadFailed')}
                  </TableCell>
                </TableRow>
              ) : table.getRowModel().rows.length ? (
                table.getRowModel().rows.map((row) => (
                  <TableRow key={row.id}>
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
        </CardContent>

        <CardFooter className="flex items-center justify-end">
          <RAGFlowPagination
            total={data?.total ?? 0}
            current={pagination.page}
            pageSize={pagination.pageSize}
            onChange={handlePageChange}
          />
        </CardFooter>
      </ScrollArea>

      {visible && activeLogId ? (
        <IngestionLogModal
          logId={activeLogId}
          visible={visible}
          onClose={hideModal}
        />
      ) : null}
    </Card>
  );
}
