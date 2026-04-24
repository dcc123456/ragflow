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
import { useTranslation } from 'react-i18next';
import { useCancelPlan } from '../hook/use-price-hooks';

interface ICancelPlanDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  planName: string;
  endDate: string;
  tenantId: string;
}

const CancelPlanDialog: React.FC<ICancelPlanDialogProps> = ({
  open,
  onOpenChange,
  planName,
  endDate,
  tenantId,
}) => {
  const { t } = useTranslation();
  const { loading: cancelLoading, cancel } = useCancelPlan();

  const handleConfirmCancel = async () => {
    const result = await cancel(tenantId);
    if (result !== undefined) {
      onOpenChange(false);
    }
  };

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="!bg-bg-base !border-border-default !text-text-primary max-w-[500px]">
        <AlertDialogHeader>
          <AlertDialogTitle>
            {t('price.cancelPlanTitle', {
              plan: planName,
              defaultValue: `Cancel ${planName} Plan?`,
            })}
          </AlertDialogTitle>
          <AlertDialogDescription className="!text-text-secondary space-y-2">
            <p>
              {t('price.cancelPlanTip', {
                plan: planName,
                defaultValue: `You are canceling the ${planName} Plan.`,
              })}
            </p>
            <p>
              {t('price.cancelPlanEffectiveTip', {
                date: endDate,
                defaultValue: `After cancellation, you can continue using the current plan benefits until ${endDate}, after which they will expire.`,
              })}
            </p>
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
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={cancelLoading}
            onClick={handleConfirmCancel}
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
