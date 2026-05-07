import Divider from '@/components/ui/divider';
import { nextLayoutRef } from '@/layouts/root-layout';
import billingService from '@/services/price';
import storagePrivate from '@/utils/authorization-private-util';
import { useState } from 'react';
import { PriceName, PriceNameMapValue } from '../constant';
import { showPriceConfirmModal } from '../global';
import { ConfirmPriceEventDetail } from '../global/hook';
import { useCharge, useFetchCurrentPlan } from '../hook/use-price-hooks';
import '../index.less';
import { ICurrentPlan, IPricePlanWithButton } from '../interface';
import CancelPlanDialog from './cancel-plan-dialog';
import PricingCardButton from './pricing-card-button';

const PricingCard = (props: IPricePlanWithButton) => {
  const {
    title,
    isPopular,
    description,
    price,
    features,
    buttonLabel,
    isUse = false,
    disabled = false,
    name: currentPlanName = '',
  } = props;
  const { loading, charge } = useCharge();
  const { data: currentPlanData } = useFetchCurrentPlan();
  const [upcomingLoading, setUpComingLoading] = useState(false);
  const [isCancelDialogOpen, setIsCancelDialogOpen] = useState(false);
  const normalizedPrice =
    currentPlanName === PriceName.Trial &&
    (price === '' || price === null || price === undefined)
      ? 0
      : Number(price);
  const shouldShowPrice =
    Number.isFinite(normalizedPrice) && normalizedPrice >= 0;

  const handleBuy = async (props: IPricePlanWithButton) => {
    if (props.isUse && currentPlanData) {
      setIsCancelDialogOpen(true);
      return;
    }
    let isUpgrade = false;
    const currentPlan: ICurrentPlan = storagePrivate.getPricePlan();

    if (props.name === 'Enterprise') {
      window.open('https://ragflow.io/contact-us', '_blank');
      return;
    }

    if (
      currentPlan &&
      PriceNameMapValue[
        currentPlan.plan_name as keyof typeof PriceNameMapValue
      ] < PriceNameMapValue[currentPlanName as keyof typeof PriceNameMapValue]
    ) {
      isUpgrade = true;
    }
    if (isUpgrade && currentPlan.price_id) {
      setUpComingLoading(true);
      const { data: upcoming } = await billingService.getUpcoming({
        old_price_id: currentPlan.price_id,
        new_price_id: props.id,
      });
      setUpComingLoading(false);
      showPriceConfirmModal({
        plan: {
          ...props,
          priceDifference: upcoming?.data?.amount_due_today,
        },
        container: nextLayoutRef.current || undefined,
      } as ConfirmPriceEventDetail);
    } else {
      charge(props);
    }
  };

  return (
    <div className="relative  group max-w-[300px]">
      <div
        className={` rounded-lg p-6 text-center border border-border-button transition-transform group-hover:scale-105 bg-bg-base text-text-primary relative z-20 group-hover:border-accent-primary h-full
          `}
        // after:absolute after:-inset-4 after:bg-gradient-to-b after:from-[#42b6ff] after:to-[#2be8aa] after:blur group-hover:after:opacity-50 after:z-10 after:opacity-0
      >
        <div className="flex justify-between items-center">
          <div className="text-2xl font-bold mb-4 text-left flex gap-1">
            {title}
            {isPopular && (
              <div className="bg-gradient-to-r from-[#42b6ff] to-[#2be8aa] rounded-sm px-1 py-0.5 text-sm h-6 font-normal text-black">
                Most Popular
              </div>
            )}
          </div>
          {/* <div className="icon">{icon?.()}</div> */}
        </div>
        <p className=" text-left line-clamp-3 h-[4.5rem]">{description}</p>
        <Divider className="!border-border-button" />
        <ul className="mb-6">
          {features.map((item) => (
            <li key={item.key} className="mb-2 text-left">
              <div className="flex items-center">
                {item.icon}
                {Number(item.value) > -1 && (
                  <span className="italic font-semibold">{item.value}</span>
                )}
                <span className="ml-2 text-xm font-normal">{item.name}</span>
              </div>
            </li>
          ))}
        </ul>
        <h3 className="text-3xl font-bold mb-6 text-left h-12">
          {shouldShowPrice && (
            <>
              <span className="text-sm mr-1">$</span>
              {normalizedPrice}
              <span className="text-sm text-text-secondary font-normal ml-1">
                /month
              </span>
            </>
          )}
        </h3>
        {currentPlanName !== PriceName.Trial && (
          <PricingCardButton
            buttonLabel={buttonLabel}
            isUse={isUse}
            disabled={disabled}
            loading={loading}
            upcomingLoading={upcomingLoading}
            onClick={() => handleBuy(props)}
          />
        )}
      </div>
      <div className="absolute -inset-2 bg-gradient-to-b from-[#42b6ff] to-[#2be8aa] blur group-hover:opacity-50 z-10 opacity-0"></div>

      <CancelPlanDialog
        open={isCancelDialogOpen}
        onOpenChange={setIsCancelDialogOpen}
        planName={title}
        endDate={
          typeof currentPlanData?.end_time === 'string'
            ? currentPlanData.end_time.split('T')[0]
            : ''
        }
        tenantId={currentPlanData?.tenant_id || ''}
        targetPriceId={props.cancelTargetPriceId}
      />
    </div>
  );
};

export default PricingCard;
