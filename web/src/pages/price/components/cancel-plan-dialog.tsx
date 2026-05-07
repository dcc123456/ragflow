import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Modal } from '@/components/ui/modal/modal';
import { useTranslation } from 'react-i18next';
import { useCancelPlan } from '../hook/use-price-hooks';

interface ICancelPlanDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  planName: string;
  endDate: string;
  tenantId: string;
  /** price_id of the plan to downgrade to (e.g. Free plan's price_id) */
  targetPriceId?: string;
}

const CancelPlanDialog: React.FC<ICancelPlanDialogProps> = ({
  open,
  onOpenChange,
  planName,
  endDate,
  tenantId,
  targetPriceId,
}) => {
  const { t } = useTranslation();
  const { loading: cancelLoading, cancel } = useCancelPlan();

  const handleConfirmDowngrade = async () => {
    if (!targetPriceId) {
      return;
    }

    const result = await cancel(tenantId, targetPriceId);
    if (result?.code === 2000) {
      const conflicts = result.data?.resource_conflicts;
      const modal = Modal.confirm({
        title: t('price.cancelFailed', { defaultValue: 'Cancel Failed' }),
        content: (
          <div className="space-y-2">
            <p className="text-text-secondary">{result.message}</p>
            {conflicts?.map((c: any, i: number) => (
              <div
                key={i}
                className="text-sm text-text-secondary border-l-2 border-accent-primary pl-3"
              >
                {c.message}
              </div>
            ))}
          </div>
        ),
        okText: t('common.confirm', { defaultValue: 'Confirm' }),
        onOk: () => modal?.destroy(),
      });
      return;
    }
    if (result !== undefined) {
      onOpenChange(false);
    }
  };

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="!bg-bg-base !border-border-default !text-text-primary max-w-[500px] p-0">
        <AlertDialogHeader>
          <AlertDialogTitle className="border-b border-border-default p-6">
            {t('price.cancelPlanTitle', {
              plan: planName,
              defaultValue: `Cancel ${planName} Plan?`,
            })}
          </AlertDialogTitle>
          <AlertDialogDescription className="!text-text-secondary space-y-2 px-6 pt-6">
            <p
              dangerouslySetInnerHTML={{
                __html: t('price.cancelPlanTip', {
                  plan: `<strong class='text-text-primary'> ${planName}</strong>`,
                  defaultValue: `You are canceling the <strong class='text-text-primary'>${planName}</strong>  Plan.`,
                }),
              }}
            ></p>
            <p
              dangerouslySetInnerHTML={{
                __html: t('price.cancelPlanEffectiveTip', {
                  date: `<strong class='text-text-primary'> ${endDate}</strong>`,
                  defaultValue: `After cancellation, you can continue using the current plan benefits until <strong class='text-text-primary'>${endDate}</strong>, after which they will expire.`,
                }),
              }}
            ></p>
            <p>
              {t('price.cancelPlanSwitchTip', {
                defaultValue:
                  'After expiration, your account will automatically switch to the Free Plan, and features, quotas, or resources beyond the Free Plan scope will no longer be available.',
              })}
            </p>
            <p>
              {t('price.cancelPlanConfirmQuestion', {
                defaultValue: 'Are you sure you want to cancel?',
              })}
            </p>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter className="px-6 pb-6">
          <AlertDialogCancel onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={cancelLoading || !targetPriceId}
            onClick={handleConfirmDowngrade}
            className="bg-state-error text-text-primary hover:bg-state-error hover:text-text-primary"
          >
            {cancelLoading && (
              <span className="inline-block me-2 h-4 w-4 animate-spin border-2 border-white border-t-transparent rounded-full" />
            )}
            {t('common.confirm')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
};

export default CancelPlanDialog;
