import { Button } from '@/components/ui/button';
import { Input, Modal, Pagination, Table, Tag, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Coins, Loader2, RefreshCw } from 'lucide-react';
import { useState } from 'react';
import { IPointHoldItem, IPointLedgerItem } from '../interface';
import {
  useFetchPointsBalance,
  useFetchPointsHolds,
  useFetchPointsLedger,
  usePointsCheckout,
} from './hook/points';

const EVENT_TYPE_COLOR: Record<string, string> = {
  recharge: 'green',
  hold_created: 'orange',
  consume: 'red',
  release: 'blue',
};

const HOLD_STATUS_COLOR: Record<string, string> = {
  held: 'orange',
  committed: 'green',
  released: 'blue',
  expired: 'default',
};

function formatTime(ms: number) {
  if (!ms) return '-';
  return new Date(ms).toLocaleString();
}

// ─── Balance Card ────────────────────────────────────────────────────────────

const BalanceCard = () => {
  const { data: balance, loading, refetch } = useFetchPointsBalance();
  const checkoutMutation = usePointsCheckout();

  const [modalOpen, setModalOpen] = useState(false);
  const [inputPoints, setInputPoints] = useState('');

  const handleBuy = () => {
    const pts = parseInt(inputPoints, 10);
    if (!pts || pts <= 0 || pts % 100 !== 0) {
      message.error(
        'Points must be a positive multiple of 100 (e.g. 100, 500, 1000)',
      );
      return;
    }
    checkoutMutation.mutate(pts, {
      onSuccess: (res) => {
        if (res?.code === 0 && res?.data?.checkout_url) {
          window.open(res.data.checkout_url, '_blank');
          setModalOpen(false);
          setInputPoints('');
        } else {
          message.error(res?.message || 'Failed to create checkout session');
        }
      },
      onError: () => {
        message.error('Request failed, please try again');
      },
    });
  };

  return (
    <div className="bg-bg-input border border-border-default rounded-lg p-5 mb-6">
      <div className="flex justify-between items-center mb-4">
        <div className="flex items-center gap-2">
          <Coins size={20} className="text-accent-primary" />
          <h2 className="text-xl font-semibold text-text-primary">
            Point Balance
          </h2>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => refetch()}
            disabled={loading}
          >
            <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
          </Button>
          <Button size="sm" onClick={() => setModalOpen(true)}>
            Buy Points
          </Button>
        </div>
      </div>

      {loading ? (
        <div className="flex items-center gap-2 text-text-secondary">
          <Loader2 className="animate-spin" size={16} />
          Loading...
        </div>
      ) : (
        <div className="grid grid-cols-3 gap-4">
          <Stat
            label="Available"
            value={balance?.available_points ?? 0}
            color="text-green-600"
          />
          <Stat
            label="Held (in-progress)"
            value={balance?.held_points ?? 0}
            color="text-orange-500"
          />
          <Stat
            label="Total"
            value={balance?.total_points ?? 0}
            color="text-text-primary"
          />
        </div>
      )}

      <Modal
        title="Buy Points"
        open={modalOpen}
        onCancel={() => {
          setModalOpen(false);
          setInputPoints('');
        }}
        footer={[
          <Button
            key="cancel"
            variant="outline"
            onClick={() => {
              setModalOpen(false);
              setInputPoints('');
            }}
          >
            Cancel
          </Button>,
          <Button
            key="buy"
            onClick={handleBuy}
            disabled={checkoutMutation.isPending}
          >
            {checkoutMutation.isPending ? (
              <>
                <Loader2 className="animate-spin mr-1" size={14} />
                Creating checkout...
              </>
            ) : (
              'Proceed to Payment'
            )}
          </Button>,
        ]}
      >
        <div className="py-4 space-y-3">
          <p className="text-text-secondary text-sm">
            Enter the number of points to purchase. 100 points = $1 USD. Amount
            must be a multiple of 100.
          </p>
          <Input
            type="number"
            min={100}
            step={100}
            placeholder="e.g. 1000"
            value={inputPoints}
            onChange={(e) => setInputPoints(e.target.value)}
            suffix="pts"
            onPressEnter={handleBuy}
          />
          {inputPoints && parseInt(inputPoints, 10) > 0 && (
            <p className="text-text-secondary text-sm">
              ≈ ${(parseInt(inputPoints, 10) / 100).toFixed(2)} USD
            </p>
          )}
        </div>
      </Modal>
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

  const columns: ColumnsType<IPointLedgerItem> = [
    {
      title: 'Time',
      dataIndex: 'create_time',
      key: 'create_time',
      width: 180,
      render: (v: number) => formatTime(v),
    },
    {
      title: 'Event',
      dataIndex: 'event_type',
      key: 'event_type',
      width: 130,
      render: (v: string) => (
        <Tag color={EVENT_TYPE_COLOR[v] ?? 'default'}>{v}</Tag>
      ),
    },
    {
      title: 'Points',
      dataIndex: 'points',
      key: 'points',
      width: 100,
      render: (v: number) => (
        <span className={v > 0 ? 'text-green-600' : 'text-red-500'}>
          {v > 0 ? `+${v}` : v}
        </span>
      ),
    },
    {
      title: 'Description',
      dataIndex: 'description',
      key: 'description',
      render: (v: string | null) => v || '-',
    },
    {
      title: 'Idempotency Key',
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
        <Pagination
          current={page}
          pageSize={pageSize}
          total={data?.total ?? 0}
          onChange={setPage}
          showSizeChanger={false}
          showTotal={(total) => `${total} records`}
        />
      </div>
    </div>
  );
};

// ─── Holds Table ──────────────────────────────────────────────────────────────

const HoldsTable = () => {
  const { data, loading, page, setPage, pageSize } = useFetchPointsHolds();

  const columns: ColumnsType<IPointHoldItem> = [
    {
      title: 'Time',
      dataIndex: 'create_time',
      key: 'create_time',
      width: 180,
      render: (v: number) => formatTime(v),
    },
    {
      title: 'Doc ID',
      dataIndex: 'doc_id',
      key: 'doc_id',
      ellipsis: true,
    },
    {
      title: 'Points',
      dataIndex: 'points',
      key: 'points',
      width: 90,
    },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      width: 110,
      render: (v: string) => (
        <Tag color={HOLD_STATUS_COLOR[v] ?? 'default'}>{v}</Tag>
      ),
    },
  ];

  return (
    <div>
      <h3 className="text-lg font-semibold text-text-primary mb-3">
        Active Holds
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
        <Pagination
          current={page}
          pageSize={pageSize}
          total={data?.total ?? 0}
          onChange={setPage}
          showSizeChanger={false}
          showTotal={(total) => `${total} records`}
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
      <HoldsTable />
    </div>
  );
};

export default PointsPage;
