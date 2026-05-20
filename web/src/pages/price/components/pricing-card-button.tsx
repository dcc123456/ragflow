import { ButtonLoading } from '@/components/ui/button';
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import classNames from 'classnames';
import { useTranslation } from 'react-i18next';
import { PriceName } from '../constant';
import { useFetchCurrentPlan } from '../hook/use-price-hooks';

interface IPricingCardButtonProps {
  buttonLabel: string;
  isUse: boolean;
  disabled: boolean;
  paymentRequired?: boolean;
  loading: boolean;
  upcomingLoading: boolean;
  onClick: () => void;
}

const PricingCardButton = (props: IPricingCardButtonProps) => {
  const {
    buttonLabel,
    isUse,
    disabled,
    paymentRequired,
    loading,
    upcomingLoading,
    onClick,
  } = props;
  const { t } = useTranslation();
  const { data: currentPlanData } = useFetchCurrentPlan();

  // The Cancel-plan button should be disabled only when a downgrade to Trial is
  // already scheduled.  A storage-only schedule (same plan, different storage
  // quantity) must not block cancellation.
  const hasPendingTrialDowngrade =
    currentPlanData?.pending_subscription_change?.pending_plan_name ===
    PriceName.Trial;

  const pendingEffectiveDate =
    typeof currentPlanData?.pending_subscription_change?.effective_at ===
    'string'
      ? currentPlanData.pending_subscription_change.effective_at.split('T')[0]
      : '';
  const disableCurrentPlanButton = isUse && paymentRequired;

  if (isUse && (hasPendingTrialDowngrade || disableCurrentPlanButton)) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="block w-full">
            <ButtonLoading
              type="button"
              className={classNames(
                'w-full py-2 rounded-lg font-bold bg-bg-card text-text-primary border border-border-button opacity-50 cursor-not-allowed',
              )}
              disabled
              loading={loading || upcomingLoading}
            >
              {buttonLabel}
            </ButtonLoading>
          </span>
        </TooltipTrigger>
        <TooltipContent>
          {disableCurrentPlanButton ? (
            <p>
              {t('billing.subscription.paymentRequired', {
                defaultValue:
                  'This subscription needs payment recovery before it can be changed or cancelled.',
              })}
            </p>
          ) : (
            <div>
              <p>
                {t('price.cancelPlanEffectiveTip', {
                  date: pendingEffectiveDate,
                  defaultValue: `After cancellation, you can continue using the current plan benefits until ${pendingEffectiveDate}, after which they will expire.`,
                })}
              </p>
              <p className="mt-1">
                {t('price.cancelPlanSwitchTip', {
                  defaultValue:
                    'If your current usage exceeds the Trial plan limits, your downgrade will be blocked and you will remain on the current plan. Existing add-on storage does not extend the Trial storage quota, and any add-on storage will be cancelled when the current plan expires.',
                })}
              </p>
            </div>
          )}
        </TooltipContent>
      </Tooltip>
    );
  }

  return (
    <ButtonLoading
      type="button"
      className={classNames(
        'w-full py-2 rounded-lg font-bold bg-bg-card text-text-primary border border-border-default  group-hover:bg-text-primary group-hover:text-text-primary-inverse group-hover:border-b-2 group-hover:border-b-[#00BEB4]',
        {
          'border border-border-button': isUse,
          'opacity-50 cursor-not-allowed': !isUse && disabled,
        },
      )}
      onClick={onClick}
      disabled={
        loading ||
        upcomingLoading ||
        disableCurrentPlanButton ||
        (!isUse && disabled)
      }
      loading={loading || upcomingLoading}
    >
      {buttonLabel}
    </ButtonLoading>
  );
};

export default PricingCardButton;
