import '../index.less';
import { BaseInfo } from './base-info';

const Overview = () => {
  // const { data: planData } = useFetchPlanOverview();
  return (
    <div className="flex flex-col gap-y-4">
      <BaseInfo />
      {/* {planData?.plan_name &&
        pricingPlans[planData?.plan_name as keyof typeof pricingPlans] ===
          pricingPlans.Pro && <PayAsYouGo />} */}
    </div>
  );
};

export { Overview };
