import { useFetchPlanOverview } from '../hook/overview';
import '../index.less';
import { BaseInfo, pricingPlans } from './base-info';
import { PayAsYouGo } from './pay-as-you-go';

const Overview = () => {
  const { data: planData } = useFetchPlanOverview();
  return (
    <div className="flex flex-col gap-y-4">
      <BaseInfo />
      {planData?.plan_name === pricingPlans.Pro && <PayAsYouGo />}
    </div>
  );
};

export { Overview };
