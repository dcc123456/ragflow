import '../index.less';
import { BaseInfo } from './base-info';
import { PayAsYouGo } from './pay-as-you-go';

const Overview = () => {
  return (
    <div className="flex flex-col gap-y-4">
      <BaseInfo />
      <PayAsYouGo />
    </div>
  );
};

export { Overview };
