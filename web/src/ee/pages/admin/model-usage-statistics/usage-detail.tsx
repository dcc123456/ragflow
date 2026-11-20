import { useTranslation } from 'react-i18next';
import type { ChangeEvent } from 'react';

import { useDebounce } from 'ahooks';
import {
  flexRender,
  getCoreRowModel,
  getPaginationRowModel,
  useReactTable,
} from '@tanstack/react-table';

import { TableEmpty, TableSkeleton } from '@/components/table-skeleton';
import { Input } from '@/components/ui/input';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import { Segmented, SegmentedValue } from '@/components/ui/segmented';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';

import { useTokenStatsDetail, useUsageDetailColumns } from './hooks';
import type { TokenStatsGranularity, TokenStatsWindow } from './types';

const GranularityOptions: TokenStatsGranularity[] = ['hour', 'day', 'month'];

interface UsageDetailProps {
  window: TokenStatsWindow;
  email: string;
  onEmailChange: (email: string) => void;
  granularity: TokenStatsGranularity;
  onGranularityChange: (granularity: TokenStatsGranularity) => void;
}

export function UsageDetail({
  window,
  email,
  onEmailChange,
  granularity,
  onGranularityChange,
}: UsageDetailProps) {
  const { t } = useTranslation();
  const debouncedEmail = useDebounce(email, { wait: 500 });
  const { data, isLoading } = useTokenStatsDetail(
    window,
    debouncedEmail,
    granularity,
  );
  const items = data ?? [];
  const columns = useUsageDetailColumns(granularity);

  const table = useReactTable({
    data: items,
    columns,
    defaultColumn: {
      minSize: 1,
      maxSize: Number.MAX_SAFE_INTEGER,
    },
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
  });

  const { pageIndex, pageSize } = table.getState().pagination;
  const isSearching = !!email && email !== debouncedEmail;

  const handleEmailChange = (event: ChangeEvent<HTMLInputElement>) => {
    onEmailChange(event.target.value);
  };

  const handleGranularityChange = (value: SegmentedValue) => {
    onGranularityChange(value as TokenStatsGranularity);
  };

  const handlePaginationChange = (page: number, size: number) => {
    table.setPagination({ pageIndex: page - 1, pageSize: size });
  };

  return (
    <section>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-base font-medium text-text-primary">
          {t('admin.modelUsage.usageDetail')}
        </h2>

        <div className="flex items-center gap-3">
          <Input
            value={email}
            onChange={handleEmailChange}
            placeholder={t('admin.modelUsage.userEmailPlaceholder')}
            className="h-8 w-[240px] bg-bg-input border-border-button"
          />

          <Segmented
            options={GranularityOptions.map((option) => ({
              value: option,
              label: t(`admin.modelUsage.${option}`),
            }))}
            value={granularity}
            onChange={handleGranularityChange}
            sizeType="sm"
          />
        </div>
      </div>

      <Table className="table-fixed">
        <colgroup>
          {table.getVisibleLeafColumns().map((column) => (
            <col key={column.id} style={{ width: `${column.getSize()}%` }} />
          ))}
        </colgroup>
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
          {!email ? (
            <TableRow>
              <TableCell
                colSpan={columns.length}
                className="h-24 text-center text-text-secondary"
              >
                {t('admin.modelUsage.selectUserHint')}
              </TableCell>
            </TableRow>
          ) : isSearching || isLoading ? (
            <TableSkeleton columnsLength={columns.length} />
          ) : table.getRowModel().rows.length ? (
            table.getRowModel().rows.map((row) => (
              <TableRow key={row.id} className="group">
                {row.getVisibleCells().map((cell) => (
                  <TableCell
                    key={cell.id}
                    className="text-text-secondary group-hover:text-text-primary"
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

      {items.length > 0 && (
        <div className="flex items-center justify-end pt-4">
          <RAGFlowPagination
            total={items.length}
            current={pageIndex + 1}
            pageSize={pageSize}
            onChange={handlePaginationChange}
          />
        </div>
      )}
    </section>
  );
}
