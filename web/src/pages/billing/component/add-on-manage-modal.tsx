import NumberInput from '@/components/originui/number-input';
import message from '@/components/ui/message';
import { Modal } from '@/components/ui/modal/modal';
import {
  StripeProvider,
  useStripe as useStripeContext,
} from '@/contexts/stripe-context';
import {
  StorageAddonSetupRetryKey,
  useSetupIntentPayment,
} from '@/pages/price/hook/use-price-hooks';
import QueryClientSingleton from '@/utils/query-client-singleton';
import {
  Elements,
  PaymentElement,
  useElements,
  useStripe,
} from '@stripe/react-stripe-js';
import { QueryClientProvider } from '@tanstack/react-query';
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { useTranslation } from 'react-i18next';

interface IOkFuncProps {
  value: number;
  paymentMethodReady?: boolean;
  setupIntentId?: string;
}

interface IStorageUpgradePreview {
  amount_due_today?: number;
  has_reusable_payment_method?: boolean;
  stripe_publishable_key?: string | null;
}
interface CustomModalProps {
  isOpen: boolean;
  onClose: () => void;
  onOk: (T: IOkFuncProps) => Promise<void> | void;
  tenantId?: string;
  defaultValue?: number;
  currentValue?: number;
  price?: number;
  getUpgradePreview?: (
    value: number,
  ) => Promise<IStorageUpgradePreview | undefined>;
}

type ConfirmSetupHandler = () => Promise<{ status?: string }>;

const SetupIntentPaymentElement = ({
  onReady,
}: {
  onReady: (handler: ConfirmSetupHandler | null) => void;
}) => {
  const stripe = useStripe();
  const elements = useElements();

  useEffect(() => {
    if (!stripe || !elements) {
      onReady(null);
      return;
    }

    onReady(async () => {
      const result = await stripe.confirmSetup({
        elements,
        confirmParams: {
          return_url: window.location.href,
        },
        redirect: 'if_required',
      });

      if (result.error) {
        throw new Error(
          result.error.message || 'Failed to confirm payment details',
        );
      }

      if (!result.setupIntent) {
        throw new Error(
          'Setup intent confirmation did not return a setup intent',
        );
      }

      if (result.setupIntent.status !== 'succeeded') {
        throw new Error(`Setup intent is ${result.setupIntent.status}`);
      }

      return result.setupIntent;
    });

    return () => onReady(null);
  }, [elements, onReady, stripe]);

  return <PaymentElement />;
};

const StorageSetupPaymentSection = ({
  clientSecret,
  onReady,
}: {
  clientSecret: string;
  onReady: (handler: ConfirmSetupHandler | null) => void;
}) => {
  const { stripe } = useStripeContext();

  if (!stripe) {
    return null;
  }

  return (
    <div className="mb-2 p-4 border border-border-button rounded-md">
      <div className="text-sm text-text-secondary mb-4">
        Please enter your payment details to complete the storage upgrade
      </div>
      <Elements
        stripe={stripe}
        options={{
          clientSecret,
          appearance: {
            theme: 'stripe',
          },
        }}
      >
        <SetupIntentPaymentElement onReady={onReady} />
      </Elements>
    </div>
  );
};

const CustomModal: React.FC<CustomModalProps> = ({
  isOpen,
  onClose,
  onOk,
  tenantId = '',
  defaultValue = 0,
  currentValue = defaultValue,
  price = 0,
  getUpgradePreview,
}) => {
  const [value, setValue] = useState(defaultValue);
  const [immediateCharge, setImmediateCharge] = useState(
    Math.max(0, (defaultValue - currentValue) * price),
  );
  const [isImmediateChargeLoading, setIsImmediateChargeLoading] =
    useState(false);
  const [loading, setLoading] = useState(false);
  const [hasReusablePaymentMethod, setHasReusablePaymentMethod] =
    useState(true);
  const [publishableKey, setPublishableKey] = useState<string | null>(null);
  const [showPaymentForm, setShowPaymentForm] = useState(false);
  const [clientSecret, setClientSecret] = useState<string | null>(null);
  const [confirmSetupHandler, setConfirmSetupHandler] =
    useState<ConfirmSetupHandler | null>(null);
  const { createSetupIntent } = useSetupIntentPayment();
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
      setHasReusablePaymentMethod(true);
      setPublishableKey(null);
      setShowPaymentForm(false);
      setClientSecret(null);
      setConfirmSetupHandler(null);
      return () => {
        cancelled = true;
      };
    }

    if (!getUpgradePreview) {
      setImmediateCharge(Math.max(0, (value - currentValue) * price));
      setIsImmediateChargeLoading(false);
      return () => {
        cancelled = true;
      };
    }

    setIsImmediateChargeLoading(true);
    getUpgradePreview(value)
      .then((preview) => {
        if (!cancelled) {
          setImmediateCharge(
            preview?.amount_due_today ??
              Math.max(0, (value - currentValue) * price),
          );
          setHasReusablePaymentMethod(
            preview?.has_reusable_payment_method ?? true,
          );
          setPublishableKey(preview?.stripe_publishable_key ?? null);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setImmediateCharge(Math.max(0, (value - currentValue) * price));
          setHasReusablePaymentMethod(true);
          setPublishableKey(null);
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
  }, [currentValue, getUpgradePreview, price, value]);

  const handleCreateSetupIntent = useCallback(async () => {
    const result = await createSetupIntent({
      setup_type: 'storage_addon',
      target_storage_bytes: Math.max(0, value) * 1000 * 1000 * 1000,
    });
    setClientSecret(result.client_secret);
    setShowPaymentForm(true);
  }, [createSetupIntent, value]);

  useEffect(() => {
    setShowPaymentForm(false);
    setClientSecret(null);
    setConfirmSetupHandler(null);
  }, [value, isOpen]);

  useEffect(() => {
    if (
      !isOpen ||
      value <= currentValue ||
      hasReusablePaymentMethod ||
      !publishableKey ||
      clientSecret ||
      loading ||
      isImmediateChargeLoading
    ) {
      return;
    }

    handleCreateSetupIntent().catch((e) => {
      message.error((e as Error)?.message || 'Failed to initialize payment');
    });
  }, [
    clientSecret,
    currentValue,
    handleCreateSetupIntent,
    hasReusablePaymentMethod,
    isImmediateChargeLoading,
    isOpen,
    loading,
    publishableKey,
    value,
  ]);

  const handleConfirmWithNewPaymentMethod = async () => {
    if (!confirmSetupHandler) {
      throw new Error('Payment form is still loading');
    }

    localStorage.setItem(
      StorageAddonSetupRetryKey,
      JSON.stringify({
        tenant_id: tenantId,
        target_storage_bytes: Math.max(0, value) * 1000 * 1000 * 1000,
        auto_retry_pending: true,
      }),
    );
    const setupIntent = await confirmSetupHandler();
    const setupIntentId =
      typeof (setupIntent as { id?: string })?.id === 'string'
        ? (setupIntent as { id?: string }).id
        : '';
    if (!setupIntentId) {
      throw new Error('Setup intent confirmation did not return an ID');
    }
    localStorage.removeItem(StorageAddonSetupRetryKey);
    return setupIntentId;
  };

  const handleOk = async () => {
    setLoading(true);
    try {
      if (value > currentValue && !hasReusablePaymentMethod) {
        if (!showPaymentForm || !clientSecret) {
          await handleCreateSetupIntent();
          return;
        }
        const setupIntentId = await handleConfirmWithNewPaymentMethod();
        await onOk?.({ value, paymentMethodReady: true, setupIntentId });
        onClose();
        return;
      }

      await onOk?.({ value });
      onClose();
    } catch (e) {
      localStorage.removeItem(StorageAddonSetupRetryKey);
      message.error((e as Error)?.message || 'Failed to update storage');
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal
      open={isOpen}
      onCancel={onClose}
      onOk={handleOk}
      closable={false}
      title={t('billing.manageAddonStorage')}
      className="!w-[500px]"
      confirmLoading={loading}
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
        {value > currentValue &&
          showPaymentForm &&
          clientSecret &&
          publishableKey && (
            <StripeProvider publishableKey={publishableKey}>
              <StorageSetupPaymentSection
                clientSecret={clientSecret}
                onReady={(handler) => {
                  setConfirmSetupHandler(() => handler);
                }}
              />
            </StripeProvider>
          )}
        {value > currentValue &&
          !hasReusablePaymentMethod &&
          !showPaymentForm && (
            <div className="text-sm text-text-secondary p-3 bg-accent-primary-5 rounded-md">
              No payment method on file. Initializing secure payment details
              form for this storage upgrade.
            </div>
          )}
      </div>
    </Modal>
  );
};

let currentModal: { destroy: () => void } | null = null;
interface IShowUpgradeTipsModalOptions {
  tenantId?: string;
  defaultValue: number;
  currentValue?: number;
  onOk: (T: IOkFuncProps) => Promise<void> | void;
  price?: number;
  getUpgradePreview?: (
    value: number,
  ) => Promise<IStorageUpgradePreview | undefined>;
}
const showAddOnManageModal = ({
  tenantId = '',
  defaultValue = 0,
  currentValue = defaultValue,
  onOk,
  price = 0,
  getUpgradePreview,
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
    <QueryClientProvider client={QueryClientSingleton.getInstance()}>
      <CustomModal
        isOpen={true}
        tenantId={tenantId}
        onOk={onOk}
        defaultValue={defaultValue}
        currentValue={currentValue}
        onClose={closeModal}
        price={price}
        getUpgradePreview={getUpgradePreview}
      />
    </QueryClientProvider>,
  );

  currentModal = { destroy: closeModal };

  return currentModal;
};

export { showAddOnManageModal };
