import Divider from '@/components/ui/divider';
import { LoadingButton } from '@/components/ui/loading-button';
import { nextLayoutRef } from '@/layouts/root-layout';
import billingService from '@/services/price';
import storagePrivate from '@/utils/authorization-private-util';
import classNames from 'classnames';
import {
  DatabaseZap,
  GitPullRequestArrow,
  LayoutGrid,
  Users,
} from 'lucide-react';
import { useState } from 'react';
import { PriceNameMapValue } from '../constant';
import { showPriceComfirmModal } from '../gobal';
import { ConfirmPriceEventDetail } from '../gobal/hook';
import { useCharge } from '../hook/use-price-hooks';
import '../index.less';
import {
  ICurrentPlan,
  IFeatureProps,
  IPricePlanWithButton,
} from '../interface';

interface ISuffixProps {
  id: number;
  icon: JSX.Element;
  text: 'apps' | 'team members' | 'GB dataset storage' | 'min API requests';
  key: keyof IFeatureProps;
}
const PricingCard = (props: IPricePlanWithButton & { name?: string }) => {
  const {
    title,
    isPopular,
    description,
    price,
    feature,
    buttonLabel,
    isUse = false,
    name: currentPlanName = '',
    icon,
  } = props;
  const suffix = [
    {
      id: 1,
      icon: (
        <LayoutGrid
          size={12}
          className="text-text-primary group-hover:text-bg-base font-normal mr-2"
        />
      ),
      text: 'Apps',
      key: 'apps',
    },
    {
      id: 2,
      icon: (
        <Users
          size={12}
          className="text-text-primary group-hover:text-bg-base font-normal mr-2"
        />
      ),
      text: 'team members',
      key: 'teamMembers',
    },
    {
      id: 3,
      icon: (
        <DatabaseZap
          size={12}
          className="text-text-primary group-hover:text-bg-base font-normal mr-2"
        />
      ),
      text: 'GB dataset storage',
      key: 'datasetStorage',
    },
    {
      id: 4,
      icon: (
        <GitPullRequestArrow
          size={12}
          className="text-text-primary group-hover:text-bg-base font-normal mr-2"
        />
      ),
      text: 'API requests/month',
      key: 'apiRequests',
    },
  ] as ISuffixProps[];
  const { loading, charge } = useCharge();
  const [upCommingLoading, setUpCommingLoading] = useState(false);

  const handleBuy = async (props: IPricePlanWithButton) => {
    let isUpgrade = false;
    const currentPlan: ICurrentPlan = storagePrivate.getPricePlan();

    if (
      currentPlan &&
      PriceNameMapValue[
        currentPlan.plan_name as keyof typeof PriceNameMapValue
      ] < PriceNameMapValue[currentPlanName as keyof typeof PriceNameMapValue]
    ) {
      isUpgrade = true;
    }
    if (isUpgrade && currentPlan.price_id) {
      setUpCommingLoading(true);
      const { data: upComming } = await billingService.getUpComming({
        old_price_id: currentPlan.price_id,
        new_price_id: props.id,
      });
      setUpCommingLoading(false);
      showPriceComfirmModal({
        plan: {
          ...props,
          priceDifference: upComming?.data?.amount_due_today,
        },
        container: nextLayoutRef.current || undefined,
      } as ConfirmPriceEventDetail);
    } else {
      charge(props);
    }
  };
  return (
    <div
      className={`group rounded-lg shadow-lg p-6 text-center border border-border-button transition-transform hover:scale-105 bg-bg-base text-text-primary hover:bg-text-primary hover:text-bg-base`}
    >
      <div className="flex justify-between items-center">
        <div className="text-2xl font-bold mb-4 text-left flex gap-1">
          {title}
          {isPopular && (
            <div className="bg-accent-primary rounded-sm px-1 py-0.5 text-sm h-6 font-normal text-white">
              Most Popular
            </div>
          )}
        </div>
        <div className="icon">{icon?.()}</div>
      </div>
      <p className=" text-left h-16">{description}</p>
      <Divider className="!border-border-button" />
      <ul className="mb-6">
        {suffix.map((item) => (
          <li key={item.id} className="mb-2 text-left">
            <div className="flex items-center">
              {item.icon}
              <span className="italic font-semibold">{feature[item.key]}</span>
              <span className="ml-2 text-xm font-normal">{item.text}</span>
            </div>
          </li>
        ))}
      </ul>
      <h3 className="text-3xl font-bold mb-6 text-left">
        <span className="text-sm mr-1">$</span>
        {price}
        <span className="text-sm text-text-secondary font-normal ml-1">
          /month
        </span>
      </h3>
      {/* bg-gradient-to-r from-gray-900 to-gray-950 */}
      <LoadingButton
        type="button"
        className={classNames(
          'w-full py-2 rounded-lg font-bold bg-bg-card text-text-primary border border-border-default  group-hover:bg-bg-base group-hover:text-text-primary group-hover:border-b-2 group-hover:border-b-[#00BEB4]',
          {
            'border border-border-button': isUse,
          },
        )}
        onClick={() => handleBuy(props)}
        disabled={loading || upCommingLoading}
        loading={loading || upCommingLoading}
      >
        {/* {(loading || upCommingLoading) && <Spin></Spin>} */}
        {buttonLabel}
      </LoadingButton>
    </div>
  );
};

export default PricingCard;
