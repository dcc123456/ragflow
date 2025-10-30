import { FolderCheck } from 'lucide-react';

const payAsYouGo = {
  pagesParsed: 30,
  paidAmount: 60,
  unpaidAmount: 3.5,
  BillingCycle: { start: '2023-07-01', end: '2023-08-01' },
};
export const PayAsYouGo = () => {
  return (
    <div className="pay-as-you-go flex flex-col justify-between w-full mb-4">
      <h2 className="text-2xl font-bold text-text-primary">Pay-As-You-Go</h2>
      <div className="flex justify-between items-center">
        <div className="text-text-secondary">
          We auto-charge every time your running total reaches US$10. No
          spending limits—grow as you go.
        </div>
        <div className="text-text-secondary">
          Billing cycle: {payAsYouGo.BillingCycle.start}-
          {payAsYouGo.BillingCycle.end}
        </div>
      </div>
      <div className="mt-4 bg-bg-input border border-border-default p-4 rounded">
        <p className="text-text-primary text-base flex gap-1 items-center">
          <FolderCheck size={16} />
          Document Parsing {payAsYouGo.pagesParsed} pages
        </p>
        <div className="relative mt-3 mb-2 flex items-center w-full border rounded-full border-border-button">
          <div className="h-8 flex items-center rounded-full w-full">
            <div
              className="h-8 min-w-96 flex items-center bg-accent-primary rounded-l-full pl-3"
              style={{
                width: `30%`,
              }}
            >
              ${payAsYouGo.paidAmount} Paid
            </div>
            <div
              className="h-8 min-w-36 flex items-center bg-accent-primary rounded-r-full unpaid pl-3 text-black"
              style={{
                width: `10%`,
              }}
            >
              ${payAsYouGo.unpaidAmount}/$10 Unpaid
            </div>
            <div className="flex flex-1 justify-center pl-7">∞ Unlimited</div>
          </div>
        </div>
        <div className="flex justify-between items-end">
          <div className="text-text-primary">
            <span className="text-xs">Total spend this cycle: </span>
            <span className="text-base">
              ${payAsYouGo.paidAmount + payAsYouGo.unpaidAmount}
            </span>
          </div>
          <div className="text-text-secondary">
            Any remaining amount under $10 will still be charged at the end of
            your billing cycle
          </div>
        </div>
      </div>
    </div>
  );
};
