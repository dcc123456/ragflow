import { cn, convertKbToGb } from '@/lib/utils';
import { Check, GitPullRequestArrow } from 'lucide-react';
import { useEffect, useState } from 'react';
import ResourceUsage from '../component/resource-usage';
import { useFetchPlanOverview } from '../hook/overview';

const pricingPlans = {
  Trial: 'Free Plan',
  Starter: 'Starter Plan',
  Pro: 'Pro Plan',
  Enterprise: 'Enterprise Plan',
};
const apiPlanList = [
  { key: 10, value: 10, unit: 'min', name: 'Free Plan' },
  { key: 100, value: 100, unit: 'min', name: 'Starter Plan' },
  { key: 1000, value: 1000, unit: 'min', name: 'Pro Plan' },
];
const planTemplate = {
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
export const BaseInfo = () => {
  const [currentPlan, setCurrentPlan] = useState(planTemplate);
  // const { data: baseData } = useFetchBaseOverview();
  const { data: planData } = useFetchPlanOverview();
  useEffect(() => {
    const plan = {
      ...planTemplate,
      name: pricingPlans[planData?.plan_name as keyof typeof pricingPlans],
      storage: {
        ...planTemplate.storage,
        used: convertKbToGb(
          (planData?.resources.plan_storage?.used || 0) +
            (planData?.resources.add_on_storage?.used || 0),
        ),
        total: convertKbToGb(
          (planData?.resources.plan_storage?.limit || 0) +
            (planData?.resources.add_on_storage?.limit || 0),
        ),
        base: convertKbToGb(planData?.resources.plan_storage?.limit || 0),
        addOn: convertKbToGb(planData?.resources.add_on_storage?.limit || 0),
      },
      apps: {
        ...planTemplate.apps,
        used: planData?.resources.apps?.used || 0,
        total: planData?.resources.apps?.limit || 0,
      },
      teamMember: {
        ...planTemplate.teamMember,
        used: planData?.resources.members?.used || 0,
        total: planData?.resources.members?.limit || 0,
      },
      apiRequests: {
        ...planTemplate.apiRequests,
        used: planData?.api_request_limits?.requests_per_minute || 0,
      },
      BillingCycle: {
        ...planTemplate.BillingCycle,
        start: planData?.billing_cycle?.start || '',
        end: planData?.billing_cycle?.end || '',
      },
    };
    setCurrentPlan(plan);
  }, [planData, setCurrentPlan]);

  return (
    <>
      <div className="flex justify-between items-center mb-4">
        <div className="flex justify-between items-center w-full">
          <h2 className="text-2xl font-bold text-text-primary">
            {currentPlan.name}
          </h2>
          <p className="text-sm text-text-secondary">
            Billing cycle: {currentPlan.BillingCycle.start} -{' '}
            {currentPlan.BillingCycle.end}
          </p>
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
        <div className="bg-bg-input border border-border-default p-4 rounded mb-4">
          <div className="flex justify-between items-center mb-2">
            <div className="flex items-center">
              <span className="mr-2">
                <div className=" rounded-sm p-1">
                  <GitPullRequestArrow size={16} />
                </div>
              </span>
              <span className="text-text-primary text-base font-normal">
                API Requests
              </span>
            </div>
            <span className="text-text-primary">
              {currentPlan.apiRequests?.used}/min
            </span>
          </div>
          <div className="grid grid-cols-3 gap-1 border border-border-button rounded-full h-8">
            {apiPlanList.map((item) => {
              return (
                <div
                  className={cn(
                    'bg-accent-primary-5 text-center rounded-full flex items-center justify-center',
                    {
                      'text-accent-primary':
                        currentPlan.apiRequests?.used === item.key,
                      'text-text-primary':
                        currentPlan.apiRequests?.used !== item.key,
                    },
                  )}
                  key={item.key}
                >
                  {item.value}/{item.unit}
                  {currentPlan.apiRequests?.used === item.key && <Check />}
                </div>
              );
            })}
          </div>
          <div className="grid grid-cols-3 gap-1 h-8">
            {apiPlanList.map((item) => {
              return (
                <div
                  className={cn(
                    ' text-center rounded-full flex items-center justify-center',
                    {
                      'text-text-primary':
                        currentPlan.apiRequests?.used === item.key,
                      'text-text-secondary':
                        currentPlan.apiRequests?.used !== item.key,
                    },
                  )}
                  key={item.key}
                >
                  {item.name}
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </>
  );
};
