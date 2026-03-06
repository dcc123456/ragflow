import { FolderCheck, Loader2 } from 'lucide-react';
import { useFetchDeepDocUsage } from '../hook/overview';

export const PayAsYouGo = () => {
  const { data, loading } = useFetchDeepDocUsage();
  const deepdoc = data?.deepdoc;

  const totalPages = (deepdoc?.pages_paid ?? 0) + (deepdoc?.pages_unpaid ?? 0);
  const totalSpend =
    (deepdoc?.amount_paid ?? 0) + (deepdoc?.amount_unpaid ?? 0);

  const paidPercent =
    totalSpend > 0
      ? Math.min(((deepdoc?.amount_paid ?? 0) / totalSpend) * 100, 90)
      : 0;
  const unpaidPercent =
    totalSpend > 0
      ? Math.min(((deepdoc?.amount_unpaid ?? 0) / totalSpend) * 100, 90)
      : 0;

  if (loading) {
    return (
      <div className="pay-as-you-go flex flex-col justify-between w-full mb-4">
        <h2 className="text-2xl font-bold text-text-primary">DeepDoc Usage</h2>
        <div className="flex items-center gap-2 mt-4 text-text-secondary">
          <Loader2 className="animate-spin" size={16} />
          Loading...
        </div>
      </div>
    );
  }

  return (
    <div className="pay-as-you-go flex flex-col justify-between w-full mb-4">
      <div className="flex justify-between items-center">
        <h2 className="text-2xl font-bold text-text-primary">DeepDoc Usage</h2>
        {data?.current_period_start && data?.current_period_end && (
          <div className="text-text-secondary">
            Billing cycle: {data.current_period_start} -{' '}
            {data.current_period_end}
          </div>
        )}
      </div>

      <div className="mt-4 bg-bg-input border border-border-default p-4 rounded">
        <p className="text-text-primary text-base flex gap-1 items-center">
          <FolderCheck size={16} />
          Document Parsing {totalPages} pages
        </p>
        <div className="relative mt-3 mb-2 flex items-center w-full border rounded-full border-border-button">
          <div className="h-8 flex items-center rounded-full w-full">
            {paidPercent > 0 && (
              <div
                className="h-8 flex items-center bg-accent-primary rounded-l-full pl-3 text-sm"
                style={{ width: `${Math.max(paidPercent, 15)}%` }}
              >
                ${deepdoc?.amount_paid ?? 0} Paid
              </div>
            )}
            {unpaidPercent > 0 && (
              <div
                className="h-8 flex items-center bg-accent-primary rounded-r-full unpaid pl-3 text-black text-sm"
                style={{ width: `${Math.max(unpaidPercent, 10)}%` }}
              >
                ${deepdoc?.amount_unpaid ?? 0} In Progress
              </div>
            )}
            {totalPages === 0 && (
              <div className="flex flex-1 justify-center text-text-secondary text-sm">
                No pages parsed this cycle
              </div>
            )}
          </div>
        </div>
        <div className="flex justify-between items-end">
          <div className="text-text-primary">
            <span className="text-xs">Total spend this cycle: </span>
            <span className="text-base">${totalSpend.toFixed(2)}</span>
          </div>
          <div className="text-text-secondary text-sm">
            {(deepdoc?.pages_unpaid ?? 0) > 0
              ? `${deepdoc?.pages_unpaid} pages currently being parsed`
              : ''}
          </div>
        </div>
      </div>
    </div>
  );
};
