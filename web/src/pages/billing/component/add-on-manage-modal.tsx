import NumberInput from '@/components/originui/number-input';
import { Modal } from '@/components/ui/modal/modal';
import React, { useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { useTranslation } from 'react-i18next';
interface IOkFuncProps {
  value: number;
}
interface CustomModalProps {
  isOpen: boolean;
  onClose: () => void;
  onOk: (T: IOkFuncProps) => void;
  defaultValue?: number;
  currentValue?: number;
  price?: number;
  getImmediateCharge?: (value: number) => Promise<number | undefined>;
}

const CustomModal: React.FC<CustomModalProps> = ({
  isOpen,
  onClose,
  onOk,
  defaultValue = 0,
  currentValue = defaultValue,
  price = 0,
  getImmediateCharge,
}) => {
  const [value, setValue] = useState(defaultValue);
  const [immediateCharge, setImmediateCharge] = useState(
    Math.max(0, (defaultValue - currentValue) * price),
  );
  const [isImmediateChargeLoading, setIsImmediateChargeLoading] =
    useState(false);
  const { t } = useTranslation();
  const handleChange = (e: number) => {
    setValue(e);
  };
  const newMonthlyCost = useMemo(() => {
    return (value * price).toFixed(2);
  }, [value, price]);
  const currentMonthlyCost = useMemo(() => {
    return (currentValue * price).toFixed(2);
  }, [currentValue, price]);
  useEffect(() => {
    let cancelled = false;

    if (value <= currentValue) {
      setImmediateCharge(0);
      setIsImmediateChargeLoading(false);
      return () => {
        cancelled = true;
      };
    }

    if (!getImmediateCharge) {
      setImmediateCharge(Math.max(0, (value - currentValue) * price));
      setIsImmediateChargeLoading(false);
      return () => {
        cancelled = true;
      };
    }

    setIsImmediateChargeLoading(true);
    getImmediateCharge(value)
      .then((amount) => {
        if (!cancelled) {
          setImmediateCharge(
            amount ?? Math.max(0, (value - currentValue) * price),
          );
        }
      })
      .catch(() => {
        if (!cancelled) {
          setImmediateCharge(Math.max(0, (value - currentValue) * price));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setIsImmediateChargeLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [currentValue, getImmediateCharge, price, value]);

  const handleOk = () => {
    onOk?.({ value });
  };

  return (
    <Modal
      open={isOpen}
      onCancel={onClose}
      onOk={handleOk}
      closable={false}
      title={t('billing.manageAddonStorage')}
      className="!w-[500px]"
    >
      <div className="flex flex-col gap-4 text-text-secondary">
        <div className="flex items-center mb-4 gap-8 text-text-primary">
          <div className="text-start">{t('billing.storageTitle')}</div>
          <div className="flex items-center gap-2">
            <NumberInput value={value} onChange={(e) => handleChange(e)} />
            {t('billing.gb')}
          </div>
        </div>
        <div className="flex items-center flex-col bg-accent-primary-5 p-4 rounded-lg gap-2 text-sm">
          <div className="flex items-center justify-between w-full">
            <div className="text-text-secondary">
              {t('billing.currentMonthlyCost')}
            </div>
            <div className="font-normal text-text-primary">
              ${currentMonthlyCost}
            </div>
          </div>
          <div className="flex items-center justify-between w-full">
            <div className=" text-text-secondary">
              {t('billing.nextMonthlyCost')}
            </div>
            <div className="font-normal text-text-primary">
              ${newMonthlyCost}
            </div>
          </div>
        </div>
        <div className="h-12">
          {value < currentValue && (
            <div className="text-sm">
              {t('billing.ensureBelow')}{' '}
              <b>
                {value}
                {t('billing.gb')}
              </b>{' '}
              {t('billing.toAvoidOverage')}
            </div>
          )}
          {value > currentValue && (
            <div
              className="text-sm"
              dangerouslySetInnerHTML={{
                __html: t('billing.payNowIncremental', {
                  amount: isImmediateChargeLoading
                    ? '...'
                    : `$${immediateCharge.toFixed(2)}`,
                  nextMonthlyCost: `$${newMonthlyCost}`,
                }),
              }}
            ></div>
          )}
        </div>
      </div>
    </Modal>
  );
};

let currentModal: { destroy: () => void } | null = null;
interface IShowUpgradeTipsModalOptions {
  defaultValue: number;
  currentValue?: number;
  onOk: (T: IOkFuncProps) => void;
  price?: number;
  getImmediateCharge?: (value: number) => Promise<number | undefined>;
}
const showAddOnManageModal = ({
  defaultValue = 0,
  currentValue = defaultValue,
  onOk,
  price = 0,
  getImmediateCharge,
}: IShowUpgradeTipsModalOptions) => {
  const rootElement = document.createElement('div');
  document.body.appendChild(rootElement);

  const reactRoot = createRoot(rootElement);
  const closeModal = () => {
    reactRoot.unmount();
    document.body.removeChild(rootElement);
    currentModal = null;
  };

  reactRoot.render(
    <CustomModal
      isOpen={true}
      onOk={onOk}
      defaultValue={defaultValue}
      currentValue={currentValue}
      onClose={closeModal}
      price={price}
      getImmediateCharge={getImmediateCharge}
    />,
  );

  currentModal = { destroy: closeModal };

  return currentModal;
};

export { showAddOnManageModal };
