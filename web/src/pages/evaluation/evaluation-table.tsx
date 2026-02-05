import {
  ColumnFiltersState,
  OnChangeFn,
  RowSelectionState,
  SortingState,
  VisibilityState,
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table';
import * as React from 'react';

import { EmptyType } from '@/components/empty/constant';
import Empty from '@/components/empty/empty';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useMemo } from 'react';

import { useFetchEvaluationRunResults } from '@/hooks/use-evaluation-request';
import { useMetricsDetailDialog } from '@/hooks/use-metrics-detail-dialog';
import { pick } from 'lodash';
import { MetricsDetailDialog } from './metrics-detail-dialog';
import {
  EvaluationResultRow,
  useEvaluationTableColumns,
} from './use-evaluation-table-columns';

type EvaluationTableProps = {
  rowSelection: Record<string, boolean>;
  setRowSelection: OnChangeFn<RowSelectionState>;
};
export function EvaluationTable({
  rowSelection,
  setRowSelection,
}: EvaluationTableProps) {
  const [sorting, setSorting] = React.useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = React.useState<ColumnFiltersState>(
    [],
  );
  const [columnVisibility, setColumnVisibility] =
    React.useState<VisibilityState>({});

  const {
    data: results,
    setPagination,
    pagination,
  } = useFetchEvaluationRunResults();

  const { detailVisible, hideDetailModal, handleShowDetail, selectedResult } =
    useMetricsDetailDialog(results);

  const columns = useEvaluationTableColumns({
    onShowDetail: handleShowDetail,
  });

  // Transform data into table rows
  const tableRows = useMemo(() => {
    if (!results?.results) return [];

    return results.results.map((item) => {
      return {
        id: item.case_id,
        question: item.variable.question,
        referenceAnswer: item.variable.reference_answer,
        modelAnswer: item.generated_answer,
        ...pick(item.metrics, [
          'context_relevance',
          'faithfulness',
          'semantic_similarity',
          'faithfulness_reason',
          'semantic_similarity_reason',
          'context_relevance_reason',
        ]),
      };
    });
  }, [results]);

  const table = useReactTable<EvaluationResultRow>({
    data: tableRows,
    columns,
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    onColumnVisibilityChange: setColumnVisibility,
    onRowSelectionChange: setRowSelection,
    getRowId: (row) => row.id,
    state: {
      sorting,
      columnFilters,
      columnVisibility,
      rowSelection: rowSelection,
    },
  });

  return (
    <div className="w-full flex flex-col flex-1 min-h-0">
      <div className="flex-1 min-h-0">
        <Table rootClassName="h-full" className="table-fixed">
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  return (
                    <TableHead
                      key={header.id}
                      style={{ width: header.getSize() }}
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
                  className="group"
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      style={{
                        width: cell.column.getSize(),
                      }}
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
                  className="h-24 text-center"
                >
                  <Empty type={EmptyType.Data} />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      <div className="pt-4">
        <RAGFlowPagination
          {...pick(pagination, 'current', 'pageSize')}
          total={results.total}
          onChange={(page, pageSize) => {
            setPagination({ page, pageSize });
          }}
        ></RAGFlowPagination>
      </div>

      {detailVisible && (
        <MetricsDetailDialog
          visible={detailVisible}
          hideModal={hideDetailModal}
          resultData={selectedResult}
        />
      )}
    </div>
  );
}
