// RequestLogs component for displaying recent request logs

import { useTranslation } from 'react-i18next';

import { flexRender } from '@tanstack/react-table';

import { TableEmpty } from '@/components/table-skeleton';
import { Button } from '@/components/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';

import { useState } from 'react';
import { useHandleExport, useRequestLogsTable } from './hooks';
import { TableToolbar } from './table-toolbar';
import type { FilterValues } from './types';
import { getSortIcon } from './utils';

export function RequestLogs() {
  const { t } = useTranslation();
  const [filters, setFilters] = useState<FilterValues>({
    view: 'users',
    timeRange: '7days',
    searchValue: '',
  });
  const { table, isLoading, pagination, setPagination } =
    useRequestLogsTable(filters);

  const { handleExport } = useHandleExport(filters, 'logs');
  const handleFilterChange = (newFilters: FilterValues) => {
    setFilters(newFilters);
  };
  return (
    <div className="mx-6 mb-6">
      {/* Request logs keeps its own toolbar for independent search */}
      <div className="pb-5">
        <TableToolbar
          title={t('admin.modelUsage.recentRequestLogs')}
          subtitle={t('admin.modelUsage.viewRecentRequestLogs')}
          filters={filters}
          onChange={handleFilterChange}
          onExport={() => handleExport()}
        />
      </div>
      {isLoading ? (
        <div className="flex items-center justify-center h-32 text-text-secondary">
          {t('common.loading', 'Loading...')}
        </div>
      ) : (
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <TableHead key={header.id}>
                    {header.isPlaceholder ? null : header.column.getCanSort() ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="-ml-3 h-8"
                        onClick={header.column.getToggleSortingHandler()}
                      >
                        {flexRender(
                          header.column.columnDef.header,
                          header.getContext(),
                        )}
                        {getSortIcon(header.column.getIsSorted())}
                      </Button>
                    ) : (
                      flexRender(
                        header.column.columnDef.header,
                        header.getContext(),
                      )
                    )}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map((row, index) => (
                <TableRow key={row.id} className="group">
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      <div
                        className={`text-text-secondary group-hover:text-text-primary`}
                      >
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext(),
                        )}
                      </div>
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : (
              <TableEmpty columnsLength={table.getAllColumns().length} />
            )}
          </TableBody>
        </Table>
      )}

      {/* <div className="flex items-center justify-end  pt-4 px-4 pb-4">
        <RAGFlowPagination
          total={pagination.total}
          current={pagination.current}
          pageSize={pagination.pageSize}
          onChange={(page, pageSize) => {
            setPagination({
              page: page - 1,
              pageSize,
            });
          }}
        />
      </div> */}
    </div>
  );
}
