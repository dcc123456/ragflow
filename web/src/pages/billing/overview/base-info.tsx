import { Button } from '@/components/ui/button';
import { convertKbToGb } from '@/lib/utils';
import { createBillingPortalSession } from '@/services/price';
import { AlertTriangle, CreditCard } from 'lucide-react';
import { useEffect, useState } from 'react';
import ResourceUsage from '../component/resource-usage';
import { useFetchPlanOverview } from '../hook/overview';

export const pricingPlans = {
  Trial: 'Free Plan',
  Starter: 'Starter Plan',
  Pro: 'Pro Plan',
  Enterprise: 'Enterprise Plan',
};

const planTemplate = {
  name: 'Starter Plan',
  BillingCycle: { start: '-', end: '-' },
  price: 0,
  storage: {
    used: 0,
    total: 0,
    base: 0,
    addOn: 0,
  },
  docParse: {
    used: 0,
    total: 0,
    base: 0,
    addOn: 0,
  },
  apps: {
    used: 0,
    total: 0,
  },
  teamMember: {
    used: 0,
    total: 0,
  },
  apiRequests: {
    used: 0,
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
        used: planData?.api_request_limits?.requests_per_month || 0,
      },
      BillingCycle: {
        ...planTemplate.BillingCycle,
        start: planData?.billing_cycle?.start || '',
        end: planData?.billing_cycle?.end || '',
      },
    };
    setCurrentPlan(plan);
  }, [planData, setCurrentPlan]);

  const handleRecoverPayment = async () => {
    if (planData?.payment_recovery_url) {
      window.open(planData.payment_recovery_url, '_blank');
      return;
    }

    const { data: res } = await createBillingPortalSession();
    const redirectUrl = res?.data?.redirect_to || res?.redirect_to;
    if (redirectUrl) {
      window.open(redirectUrl, '_blank');
    }
  };

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
      {planData?.payment_required && (
        <div className="mb-4 flex items-center justify-between gap-3 rounded border border-red-500/40 bg-red-500/10 px-4 py-3 text-text-primary">
          <div className="flex items-center gap-2">
            <AlertTriangle size={18} className="text-red-500" />
            <span className="text-sm">
              Your subscription payment needs attention.
            </span>
          </div>
          <Button size="sm" onClick={handleRecoverPayment}>
            <CreditCard size={16} />
            {planData.payment_recovery_url ? 'Pay invoice' : 'Update payment'}
          </Button>
        </div>
      )}
      <div className="grid grid-cols-2 gap-4">
        <ResourceUsage
          title="Storage"
          value={currentPlan.storage?.used || 0}
          planName={currentPlan.name}
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
        <ResourceUsage
          title="Document Parse"
          value={currentPlan.docParse?.used || 0}
          planName={currentPlan.name}
          planValue={currentPlan.docParse?.base}
          limit={currentPlan.docParse?.total}
          unit="pts"
          basicCapacity={currentPlan.docParse?.base}
          showValue={false}
        ></ResourceUsage>
        {/* <div className="bg-bg-input border border-border-default p-4 rounded mb-4">
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
              {currentPlan.apiRequests?.used >= UNLIMITED_API_REQUESTS
                ? 'Unlimited'
                : `${currentPlan.apiRequests?.used}/month`}
            </span>
          </div>
          <div className="grid grid-cols-3 gap-1 border border-border-button rounded-full h-8">
            {apiPlanList.map((item) => {
              const isActive = planData?.plan_name === item.planKey;
              const label =
                item.value >= UNLIMITED_API_REQUESTS
                  ? 'Unlimited'
                  : `${item.value}/${item.unit}`;
              return (
                <div
                  className={cn(
                    'bg-accent-primary-5 text-center rounded-full flex items-center justify-center gap-1',
                    {
                      'text-accent-primary': isActive,
                      'text-text-primary': !isActive,
                    },
                  )}
                  key={item.planKey}
                >
                  {label}
                  {isActive && <Check size={14} />}
                </div>
              );
            })}
          </div>
          <div className="grid grid-cols-3 gap-1 h-8">
            {apiPlanList.map((item) => {
              const isActive = planData?.plan_name === item.planKey;
              return (
                <div
                  className={cn(
                    ' text-center rounded-full flex items-center justify-center',
                    {
                      'text-text-primary': isActive,
                      'text-text-secondary': !isActive,
                    },
                  )}
                  key={item.planKey}
                >
                  {item.name}
                </div>
              );
            })}
          </div>
        </div> */}
      </div>
    </>
  );
};
