import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import message from '@/components/ui/message';
import { Modal } from '@/components/ui/modal/modal';
import { formatNumber } from '@/pages/admin/model-usage-statistics/utils';
import { getBillingPointsPrice } from '@/services/price';
import { Coins, DollarSign, Loader2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { usePointsCheckout } from '../points/hook/points';

interface BuyPointsModalProps {
  visible: boolean;
  onClose: () => void;
  currentPoints: number;
}

interface PointsPriceInfo {
  price_id: string;
  price_usd: number | null;
  points_per_unit: number;
}

const DEFAULT_POINTS_PER_UNIT = 100;
const QUICK_SELECT_DOLLARS = [10, 20, 30, 40, 50];

const BuyPointsModal: React.FC<BuyPointsModalProps> = ({
  visible,
  onClose,
  currentPoints = 0,
}) => {
  const { t } = useTranslation();
  const [amount, setAmount] = useState<number | ''>(10);
  const [selectedQuickOption, setSelectedQuickOption] = useState<number | null>(
    0,
  );
  const [pointsPriceInfo, setPointsPriceInfo] =
    useState<PointsPriceInfo | null>(null);
  const [loadingPrice, setLoadingPrice] = useState(false);
  const checkoutMutation = usePointsCheckout();

  const pointsPerUnit =
    pointsPriceInfo?.points_per_unit || DEFAULT_POINTS_PER_UNIT;

  useEffect(() => {
    if (visible) {
      setLoadingPrice(true);
      getBillingPointsPrice()
        .then((res: any) => {
          if (res?.data?.code === 0 && res?.data?.data) {
            setPointsPriceInfo(res.data.data);
          }
        })
        .catch(() => {
          // Use defaults
        })
        .finally(() => {
          setLoadingPrice(false);
        });
    }
  }, [visible]);

  const calculatedPoints = useMemo(() => {
    return (amount === '' ? 0 : amount) * pointsPerUnit;
  }, [amount, pointsPerUnit]);

  const quickSelectPoints = useMemo(() => {
    return QUICK_SELECT_DOLLARS.map((dollars) => ({
      dollars,
      points: dollars * pointsPerUnit,
    }));
  }, [pointsPerUnit]);

  const handleQuickSelect = (dollars: number, index: number) => {
    setSelectedQuickOption(index);
    setAmount(dollars);
  };

  const handleAmountChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setSelectedQuickOption(null);
    const val = e.target.value as any;
    if (val === '' || val === 0) {
      setAmount('');
    } else {
      const num = Math.trunc(Number(val));
      setAmount(Number.isNaN(num) ? '' : num);
    }
  };

  const handleAmountBlur = () => {
    if (amount === '') {
      setAmount(0);
    }
  };

  const handleAmountKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === '.' || e.key === ',') {
      e.preventDefault();
    }
  };

  const handleConfirm = () => {
    if (amount === '' || amount <= 0) {
      message.error(t('billing.buyPointsMinError'));
      return;
    }
    checkoutMutation.mutate(amount, {
      onSuccess: (res) => {
        if (res?.code === 0 && res?.data?.checkout_url) {
          window.open(res.data.checkout_url, '_blank');
          onClose();
          setAmount(10);
          setSelectedQuickOption(0);
        } else {
          message.error(res?.message || t('billing.buyPointsFailed'));
        }
      },
      onError: () => {
        message.error(t('billing.buyPointsRequestFailed'));
      },
    });
  };

  const handleClose = () => {
    onClose();
    setAmount(10);
    setSelectedQuickOption(0);
  };

  return (
    <Modal
      open={visible}
      onOpenChange={(open) => !open && handleClose()}
      title={t('billing.buyCredits')}
      className="!w-[640px]"
      footer={
        <div className="flex justify-between items-center w-full p-5">
          <div className="flex items-center justify-between">
            <span className="text-sm text-text-secondary">
              {t('billing.youWillReceive')}
            </span>
            <span className="text-sm font-semibold text-accent-primary ml-1">
              {calculatedPoints?.toLocaleString()} {t('billing.points')}
            </span>
          </div>
          <div className="flex justify-end gap-3">
            <Button variant="outline" onClick={handleClose}>
              {t('common.cancel')}
            </Button>
            <Button
              onClick={handleConfirm}
              disabled={
                checkoutMutation.isPending ||
                amount === '' ||
                amount <= 0 ||
                loadingPrice
              }
            >
              {checkoutMutation.isPending ? (
                <>
                  <Loader2 className="animate-spin me-2 h-4 w-4" />
                  {t('billing.buyPointsCreating')}
                </>
              ) : (
                t('common.confirm')
              )}
            </Button>
          </div>
        </div>
      }
    >
      <div className="space-y-6 px-5">
        {/* Current Points Display */}
        <div className="bg-accent-primary/10 rounded-lg p-4 space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-text-secondary flex items-center gap-2">
              <Coins className="w-4 h-4" />
              {t('billing.currentPoints')}
            </span>
            <span className="text-text-primary font-medium">
              {formatNumber(currentPoints)}
            </span>
          </div>
        </div>

        {/* Quick Select */}
        <div>
          <p className="text-base font-medium text-text-primary mb-3">
            {t('billing.quickSelectTip', {
              ratio: `$ 1 = ${pointsPerUnit} ${t('billing.points')}`,
            })}
            <span className="text-xs font-normal text-text-secondary ml-3">
              {`$ 1 = ${pointsPerUnit} ${t('billing.points')}`}
            </span>
          </p>
          <div className="grid grid-cols-3 gap-x-10 gap-y-5">
            {quickSelectPoints.map((option, index) => (
              <button
                key={option.dollars}
                onClick={() => handleQuickSelect(option.dollars, index)}
                className={`
                  flex flex-col items-start justify-start p-3 rounded-lg border transition-all
                  ${
                    selectedQuickOption === index
                      ? ' bg-text-primary text-text-primary-inverse'
                      : 'border-border-default hover:border-accent-primary/50 hover:bg-bg-input'
                  }
                `}
              >
                <span className="text-lg font-semibold">${option.dollars}</span>
                <span className="text-xs text-text-secondary">
                  {option.points.toLocaleString()} {t('billing.points')}
                </span>
              </button>
            ))}
          </div>
        </div>

        {/* Custom Amount */}
        <div className="flex justify-between items-center w-full gap-x-5">
          <span className="text-sm text-text-secondary whitespace-nowrap w-40">
            {t('billing.customAmount')}
          </span>
          <div className="flex items-center flex-1">
            {/* Replace with D:\work\ragflow_enterprise\web\src\components\originui\number-input.tsx */}
            <Input
              type="number"
              prefix={
                <div className="px-2 border-r border-border-default mr-3">
                  <DollarSign className="w-4 h-4 text-text-secondary" />
                </div>
              }
              rootClassName="w-full"
              min={1}
              max={10000}
              step={1}
              value={amount}
              onChange={handleAmountChange}
              onBlur={handleAmountBlur}
              onKeyDown={handleAmountKeyDown}
            />
          </div>
          <div className="text-sm text-text-secondary bg-bg-card rounded-sm px-3 py-1.5 w-32">
            {calculatedPoints.toLocaleString()} {t('billing.points')}
          </div>
        </div>
      </div>
    </Modal>
  );
};

export default BuyPointsModal;
