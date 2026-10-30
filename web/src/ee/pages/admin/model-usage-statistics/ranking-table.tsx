// RankingTable component for displaying user/department/group rankings

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

import { memo } from 'react';
import { useRankingTable } from './hooks';
import type { FilterValues } from './types';
import { getSortIcon } from './utils';

interface RankingTableProps {
  filters: FilterValues;
  // onExport: (data: unknown[]) => void;
}

const RankingTable = memo(({ filters }: RankingTableProps) => {
  const { t } = useTranslation();

  const { table, isLoading, pagination, setPagination } =
    useRankingTable(filters);

  return (
    <div className="mx-6">
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
                <TableRow key={row.id} className={'group'}>
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      className="text-text-secondary group-hover:text-text-primary"
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
              <TableEmpty columnsLength={table.getAllColumns().length} />
            )}
          </TableBody>
        </Table>
      )}

      {/* <div className="flex items-center justify-end border-t border-border-button pt-4 px-4 pb-4">
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
});

RankingTable.displayName = 'RankingTable';

export { RankingTable };
