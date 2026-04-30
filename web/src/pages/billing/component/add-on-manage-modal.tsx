import NumberInput from '@/components/originui/number-input';
import { Modal } from '@/components/ui/modal/modal';
import { formatPureDate } from '@/utils/date';
import React, { useMemo } from 'react';
import { createRoot } from 'react-dom/client';
import { useTranslation } from 'react-i18next';

interface IAbandonPendingModalProps {
  isOpen: boolean;
  onClose: () => void;
  onPayNow: () => void;
  onAbandon: () => Promise<void>;
  pendingQuantityGb: number;
  targetQuantityGb: number;
}

const AbandonPendingModal: React.FC<IAbandonPendingModalProps> = ({
  isOpen,
  onClose,
  onPayNow,
  onAbandon,
  pendingQuantityGb,
  targetQuantityGb,
}) => {
  const { t } = useTranslation();
  const [loading, setLoading] = React.useState(false);

  const handleAbandon = async () => {
    setLoading(true);
    await onAbandon();
    setLoading(false);
  };

  return (
    <Modal
      open={isOpen}
      onCancel={onClose}
      closable={false}
      title={t('billing.pendingIncreaseTitle')}
      className="!w-[480px]"
      footer={
        <div className="flex justify-end gap-2 pt-2">
          <button
            className="px-4 py-2 rounded border border-border-button text-sm text-text-primary hover:bg-bg-input"
            onClick={onClose}
          >
            {t('common.cancel')}
          </button>
          <button
            className="px-4 py-2 rounded border border-border-button text-sm text-text-primary hover:bg-bg-input"
            onClick={onPayNow}
          >
            {t('billing.payNow')}
          </button>
          <button
            disabled={loading}
            className="px-4 py-2 rounded bg-accent-primary text-white text-sm hover:opacity-90 disabled:opacity-50"
            onClick={handleAbandon}
          >
            {t('billing.abandonAndApply', {
              target_quantity_bytes: targetQuantityGb,
            })}
          </button>
        </div>
      }
    >
      <p className="text-sm text-text-secondary">
        {t('billing.pendingIncreaseDescription', {
          pending_quantity_bytes: pendingQuantityGb,
          target_quantity_bytes: targetQuantityGb,
        })}
      </p>
    </Modal>
  );
};

interface IShowAbandonPendingModalOptions {
  pendingQuantityGb: number;
  targetQuantityGb: number;
  invoiceUrl: string;
  onAbandon: () => Promise<void>;
}

const showAbandonPendingModal = ({
  pendingQuantityGb,
  targetQuantityGb,
  invoiceUrl,
  onAbandon,
}: IShowAbandonPendingModalOptions) => {
  const rootElement = document.createElement('div');
  document.body.appendChild(rootElement);
  const reactRoot = createRoot(rootElement);

  const closeModal = () => {
    reactRoot.unmount();
    document.body.removeChild(rootElement);
  };

  const handlePayNow = () => {
    if (invoiceUrl) window.open(invoiceUrl);
    closeModal();
  };

  const handleAbandon = async () => {
    await onAbandon();
    closeModal();
  };

  reactRoot.render(
    <AbandonPendingModal
      isOpen={true}
      onClose={closeModal}
      onPayNow={handlePayNow}
      onAbandon={handleAbandon}
      pendingQuantityGb={pendingQuantityGb}
      targetQuantityGb={targetQuantityGb}
    />,
  );
};
interface IOkFuncProps {
  value: number;
}
interface CustomModalProps {
  isOpen: boolean;
  onClose: () => void;
  onOk: (T: IOkFuncProps) => void;
  defaultValue?: number;
  price?: number;
  decreaseEffectiveAt?: string | null;
}

const CustomModal: React.FC<CustomModalProps> = ({
  isOpen,
  onClose,
  onOk,
  defaultValue = 0,
  price = 0,
  decreaseEffectiveAt = null,
}) => {
  const [value, setValue] = React.useState(defaultValue);
  const { t } = useTranslation();
  const handleChange = (e: number) => {
    setValue(e);
  };
  const newCost = useMemo(() => {
    return (value * price).toFixed(2);
  }, [value, price]);
  const handleOk = () => {
    onOk?.({ value });
  };

  const decreaseEffectiveDay = useMemo(() => {
    if (decreaseEffectiveAt) {
      return formatPureDate(decreaseEffectiveAt);
    }
    return '';
  }, [decreaseEffectiveAt]);
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
              ${defaultValue * price}
            </div>
          </div>
          <div className="flex items-center justify-between w-full">
            <div className=" text-text-secondary">
              {t('billing.newMonthlyCost')}
            </div>
            <div className="font-normal text-text-primary">${newCost}</div>
          </div>
        </div>
        <div className="h-12">
          {value < defaultValue && (
            <div>
              <div className="text-sm">
                {t('billing.reducedQuotaEffective')}{' '}
                {decreaseEffectiveDay ? <b>{decreaseEffectiveDay}</b> : null}
                {decreaseEffectiveDay ? '.' : null}
              </div>
              <div className="text-sm">
                {t('billing.ensureBelow')}{' '}
                <b>
                  {value}
                  {t('billing.gb')}
                </b>{' '}
                {t('billing.toAvoidOverage')}
              </div>
            </div>
          )}
          {value > defaultValue && (
            <div className="text-sm">
              {t('billing.payNowProrated', {
                amount: (Number(newCost) - defaultValue * price).toFixed(2),
              })}
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
};

let currentModal: { destroy: () => void } | null = null;
interface IShowUpgradeTipsModalOptions {
  defaultValue: number;
  onOk: (T: IOkFuncProps) => void;
  price?: number;
  decreaseEffectiveAt?: string | null;
}
const showAddOnManageModal = ({
  defaultValue = 0,
  onOk,
  price = 0,
  decreaseEffectiveAt = null,
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
      onClose={closeModal}
      price={price}
      decreaseEffectiveAt={decreaseEffectiveAt}
    />,
  );

  currentModal = { destroy: closeModal };

  return currentModal;
};

export { showAbandonPendingModal, showAddOnManageModal };
