import Space from '@/components/ui/space';
import { Check, GitPullRequestArrow } from 'lucide-react';
import UpgradeButton from '../price/price-modal/to-upgrade-button';
import ResourceUsage from './component/resource-usage';
import './index.less';

const Overview = () => {
  const payAsYouGo = {
    pagesParsed: 30,
    paidAmount: 60,
    unpaidAmount: 3.5,
  };
  const apiPlanList = [
    { key: 10, value: 10, unit: 'min' },
    { key: 100, value: 100, unit: 'min' },
    { key: 1000, value: 1000, unit: 'min' },
  ];
  const currentPlan = {
    name: 'Starter Plan',
    BillingCycle: { start: '2023-04-28', end: '2023-05-28' },
    price: 0,
    storage: {
      used: 5,
      total: 50,
      base: 10,
      addOn: 40,
    },
    apps: {
      used: 5,
      total: 10,
    },
    teamMember: {
      used: 5,
      total: 50,
    },
    apiRequests: {
      used: 100,
    },
  };
  return (
    <div className="flex flex-col gap-y-4">
      <div className="flex justify-between items-center mb-4">
        <Space align="end">
          <h2 className="text-2xl font-bold text-white">{currentPlan.name}</h2>
          <p className="text-sm text-gray-400">
            Billing cycle: {currentPlan.BillingCycle.start} -{' '}
            {currentPlan.BillingCycle.end}
          </p>
        </Space>
        <div>
          <span className="text-gray-400 mr-4">Need more?</span>
          <UpgradeButton />
        </div>
      </div>
      <div className="grid grid-cols-2 gap-4">
        <ResourceUsage
          title="Storage"
          value={currentPlan.storage?.used || 0}
          planName="starter"
          planValue={currentPlan.storage?.base}
          limit={currentPlan.storage?.total}
          unit="GB"
          basicCapacity={currentPlan.storage?.base}
        ></ResourceUsage>
        <ResourceUsage
          title="Apps"
          value={currentPlan.apps?.used}
          limit={currentPlan.apps?.total}
        />
        <ResourceUsage
          title="Team Member"
          value={currentPlan.teamMember?.used}
          limit={currentPlan.teamMember?.total}
        ></ResourceUsage>
        <div className="bg-background-card p-4 rounded mb-4">
          <div className="flex justify-between items-center mb-2">
            <div className="flex items-center">
              <span className="mr-2">
                <div className="border rounded-sm p-1">
                  <GitPullRequestArrow />
                </div>
              </span>
              <span className="text-white text-lg font-semibold">
                API Requests
              </span>
            </div>
            <span className="text-white">
              {currentPlan.apiRequests?.used}/min
            </span>
          </div>
          <div className="grid grid-cols-3 gap-1 border border-gray-500 rounded-full h-8">
            {apiPlanList.map((item) => {
              return (
                <div
                  className="bg-[rgba(76,164,231,0.05)] text-center rounded-full flex items-center justify-center text-[#4CA4E7]"
                  key={item.key}
                >
                  {item.value}/{item.unit}
                  {currentPlan.apiRequests?.used === item.key && <Check />}
                </div>
              );
            })}
          </div>
        </div>
      </div>
      <div className="pay-as-you-go flex flex-col justify-between w-full mb-4">
        <h2 className="text-2xl font-bold text-white">Pay-As-You-Go</h2>
        <div className="flex justify-between items-center">
          <div className="text-muted-foreground">
            We auto-charge every time your running total reaches US$10. No
            spending limits—grow as you go.
          </div>
          <div className="text-muted-foreground">
            Billing cycle: {currentPlan.BillingCycle.start}-
            {currentPlan.BillingCycle.end}
          </div>
        </div>
        <div className="mt-4 bg-background-card p-4 rounded">
          <p className="text-white">
            Document Parsing {payAsYouGo.pagesParsed} pages
          </p>
          <div className="relative my-6 flex items-center w-full">
            <div className="h-8 flex items-center bg-gray-600 rounded-full w-full">
              <div
                className="h-8 min-w-96 flex items-center bg-sky-500 rounded-l-full pl-3"
                style={{
                  width: `30%`,
                }}
              >
                ${payAsYouGo.paidAmount} Paid
              </div>
              <div
                className="h-8 min-w-36 flex items-center bg-sky-500 rounded-r-full unpaid pl-3"
                style={{
                  width: `10%`,
                }}
              >
                ${payAsYouGo.unpaidAmount}/$10 Unpaid
              </div>
              <div className="pl-7">∞ Unlimited</div>
            </div>
          </div>
          <div className="flex justify-between items-end">
            <div className="text-white mt-2">
              <span className="text-base">Total spend this cycle: </span>
              <span className="text-xl">
                ${payAsYouGo.paidAmount + payAsYouGo.unpaidAmount}
              </span>
            </div>
            <div className="text-muted-foreground">
              Any remaining amount under $10 will still be charged at the end of
              your billing cycle
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export { Overview };
