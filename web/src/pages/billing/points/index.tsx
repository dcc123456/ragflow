import { Button } from '@/components/ui/button';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { formatIsoDateTime } from '@/utils/date';
import {
  ColumnDef,
  ColumnFiltersState,
  SortingState,
  flexRender,
  getCoreRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table';
import { Coins, Loader2, RefreshCw } from 'lucide-react';
import * as React from 'react';
import { useFetchPlanOverview } from '../hook/overview';
import { IPointLedgerItem } from '../interface';
import { useFetchPointsLedger } from './hook/points';

const EVENT_TYPE_LABEL: Record<string, string> = {
  recharge: 'Recharge',
  hold_created: 'Pending',
  consume: 'Consume',
  release: 'Release',
};

const EVENT_TYPE_COLOR: Record<string, string> = {
  recharge: '#00BEB4',
  hold_created: '#FAAD14',
  consume: '#3BA05C',
  release: '#00BEB4',
};

// ─── Balance Card ────────────────────────────────────────────────────────────

const BalanceCard = () => {
  const { data: planOverview, loading, refetch } = useFetchPlanOverview();
  const planPoints = planOverview?.resources?.plan_points;
  const addonPoints = planOverview?.resources?.addon_points;
  const planRemaining = Math.max(
    0,
    (planPoints?.limit ?? 0) - (planPoints?.used ?? 0),
  );
  const addonRemaining = Math.max(
    0,
    (addonPoints?.limit ?? 0) - (addonPoints?.used ?? 0),
  );
  const totalRemaining = planRemaining + addonRemaining;

  return (
    <div className="bg-bg-input border border-border-default rounded-lg p-5 mb-6">
      <div className="flex justify-between items-center mb-4">
        <div className="flex items-center gap-2">
          <Coins size={20} className="text-accent-primary" />
          <h2 className="text-xl font-semibold text-text-primary">
            Point Balance
          </h2>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => refetch()}
          disabled={loading}
        >
          <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
        </Button>
      </div>

      {loading ? (
        <div className="flex items-center gap-2 text-text-secondary">
          <Loader2 className="animate-spin" size={16} />
          Loading...
        </div>
      ) : (
        <div className="grid grid-cols-3 gap-4">
          <Stat
            label="Plan Remaining"
            value={planRemaining}
            color="text-text-primary"
          />
          <Stat
            label="Addon Remaining"
            value={addonRemaining}
            color="text-text-primary"
          />
          <Stat
            label="Total Remaining"
            value={totalRemaining}
            color="text-text-primary"
          />
        </div>
      )}
    </div>
  );
};

const Stat = ({
  label,
  value,
  color,
}: {
  label: string;
  value: number;
  color: string;
}) => (
  <div className="text-center">
    <div className={`text-3xl font-bold ${color}`}>
      {value.toLocaleString()}
    </div>
    <div className="text-text-secondary text-sm mt-1">{label}</div>
  </div>
);

// ─── Ledger Table ─────────────────────────────────────────────────────────────

const LedgerTable = () => {
  const { data, loading, page, setPage, pageSize } = useFetchPointsLedger();
  const [sorting, setSorting] = React.useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = React.useState<ColumnFiltersState>(
    [],
  );

  const columns: ColumnDef<IPointLedgerItem>[] = [
    {
      accessorKey: 'create_time',
      header: 'Time',
      cell: ({ getValue }) => formatIsoDateTime(getValue() as number),
    },
    {
      accessorKey: 'event_type',
      header: 'Event',
      cell: ({ getValue }) => {
        const v = getValue() as string;
        return (
          <span style={{ color: EVENT_TYPE_COLOR[v] ?? 'inherit' }}>
            {EVENT_TYPE_LABEL[v] ?? v}
          </span>
        );
      },
    },
    {
      accessorKey: 'points',
      header: 'Points',
      cell: ({ getValue }) => {
        const v = getValue() as number;
        return v > 0 ? `+${v}` : v;
      },
    },
    {
      accessorKey: 'description',
      header: 'Description',
      cell: ({ getValue, row }) => {
        const v = getValue() as string | null;
        if (row.original.metadata?.doc_id) {
          const pageRange = row.original.metadata.page_range
            ? ` (${row.original.metadata.page_range})`
            : '';
          return `${row.original.metadata.doc_id}${pageRange}`;
        }
        return v || '-';
      },
    },
    {
      accessorKey: 'idempotency_key',
      header: 'Event Key',
    },
  ];

  const table = useReactTable({
    data: data?.items ?? [],
    columns,
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getRowId: (row) => row.id,
    state: {
      sorting,
      columnFilters,
      pagination: { pageIndex: page - 1, pageSize },
    },
    pageCount: Math.ceil((data?.total ?? 0) / pageSize),
  });

  return (
    <div className="mb-6">
      <h3 className="text-lg font-semibold text-text-primary mb-3">
        Ledger History
      </h3>
      <Table className="rounded-lg">
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
          {loading ? (
            <TableRow>
              <TableCell colSpan={columns.length} className="h-24 text-center">
                <div className="flex items-center justify-center gap-2 text-text-secondary">
                  <Loader2 className="animate-spin" size={16} />
                  Loading...
                </div>
              </TableCell>
            </TableRow>
          ) : table.getRowModel().rows?.length ? (
            table.getRowModel().rows.map((row) => (
              <TableRow key={row.id}>
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id}>
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </TableCell>
                ))}
              </TableRow>
            ))
          ) : (
            <TableRow>
              <TableCell colSpan={columns.length} className="h-24 text-center">
                No data
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <div className="flex justify-end mt-3">
        <RAGFlowPagination
          current={page}
          pageSize={pageSize}
          total={data?.total ?? 0}
          onChange={(newPage) => setPage(newPage)}
          showSizeChanger={false}
        />
      </div>
    </div>
  );
};

// ─── Points Page ──────────────────────────────────────────────────────────────

const PointsPage = () => {
  return (
    <div className="w-full">
      <BalanceCard />
      <LedgerTable />
    </div>
  );
};

export default PointsPage;
