import { Button } from '@/components/ui/button';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination.tsx';
import { formatIsoDateTime } from '@/utils/date';
import { Table } from 'antd';
import { Coins, Loader2, RefreshCw } from 'lucide-react';
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

  const columns = [
    {
      title: 'Time',
      dataIndex: 'create_time',
      key: 'create_time',
      width: 180,
      render: (v: number) => formatIsoDateTime(v),
    },
    {
      title: 'Event',
      dataIndex: 'event_type',
      key: 'event_type',
      width: 130,
      render: (v: string) => (
        <span style={{ color: EVENT_TYPE_COLOR[v] ?? 'inherit' }}>
          {EVENT_TYPE_LABEL[v] ?? v}
        </span>
      ),
    },
    {
      title: 'Points',
      dataIndex: 'points',
      key: 'points',
      width: 100,
      render: (v: number) => (v > 0 ? `+${v}` : v),
    },
    {
      title: 'Description',
      dataIndex: 'description',
      key: 'description',
      render: (v: string | null, record: IPointLedgerItem) => {
        if (record.metadata?.doc_id) {
          const page = record.metadata.page_range
            ? ` (${record.metadata.page_range})`
            : '';
          return `${record.metadata.doc_id}${page}`;
        }
        return v || '-';
      },
    },
    {
      title: 'Event Key',
      dataIndex: 'idempotency_key',
      key: 'idempotency_key',
      ellipsis: true,
    },
  ];

  return (
    <div className="mb-6">
      <h3 className="text-lg font-semibold text-text-primary mb-3">
        Ledger History
      </h3>
      <Table
        rowKey="id"
        columns={columns}
        dataSource={data?.items ?? []}
        loading={loading}
        pagination={false}
        size="small"
        className="border border-border-default rounded-lg"
      />
      <div className="flex justify-end mt-3">
        <RAGFlowPagination
          current={page}
          pageSize={pageSize}
          total={data?.total ?? 0}
          onChange={setPage}
          showSizeChanger={false}
          // showTotal={(total) => `${total} records`}
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
