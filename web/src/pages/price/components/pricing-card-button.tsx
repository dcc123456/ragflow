import { ButtonLoading } from '@/components/ui/button';
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import classNames from 'classnames';
import { isEmpty } from 'lodash';
import { useTranslation } from 'react-i18next';
import { useFetchCurrentPlan } from '../hook/use-price-hooks';

interface IPricingCardButtonProps {
  buttonLabel: string;
  isUse: boolean;
  disabled: boolean;
  loading: boolean;
  upcomingLoading: boolean;
  onClick: () => void;
}

const PricingCardButton = (props: IPricingCardButtonProps) => {
  const { buttonLabel, isUse, disabled, loading, upcomingLoading, onClick } =
    props;
  const { t } = useTranslation();
  const { data: currentPlanData } = useFetchCurrentPlan();

  const hasPendingChange = !isEmpty(
    currentPlanData?.pending_subscription_change,
  );

  const pendingEffectiveDate =
    typeof currentPlanData?.pending_subscription_change?.effective_at ===
    'string'
      ? currentPlanData.pending_subscription_change.effective_at.split('T')[0]
      : '';

  if (isUse && hasPendingChange) {
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
                  'After expiration, your account will automatically switch to the Free Plan, and features, quotas, or resources beyond the Free Plan scope will no longer be available.',
              })}
            </p>
          </div>
        </TooltipContent>
      </Tooltip>
    );
  }

  return (
    <ButtonLoading
      type="button"
      className={classNames(
        'w-full py-2 rounded-lg font-bold bg-bg-card text-text-primary border border-border-default  group-hover:bg-bg-base group-hover:text-text-primary group-hover:border-b-2 group-hover:border-b-[#00BEB4]',
        {
          'border border-border-button': isUse,
          'opacity-50 cursor-not-allowed': !isUse && disabled,
        },
      )}
      onClick={onClick}
      disabled={loading || upcomingLoading || (!isUse && disabled)}
      loading={loading || upcomingLoading}
    >
      {buttonLabel}
    </ButtonLoading>
  );
};

export default PricingCardButton;
