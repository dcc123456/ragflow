import message from '@/components/ui/message';
import { Modal } from '@/components/ui/modal/modal';
import Space from '@/components/ui/space';
import { useStripe as useStripeContext } from '@/contexts/stripe-context';
import {
  Elements,
  PaymentElement,
  useElements,
  useStripe,
} from '@stripe/react-stripe-js';
import { memo, useCallback, useEffect, useState } from 'react';
import {
  getNextMonth,
  TrialUpgradeSetupRetryKey,
  useCharge,
  useSetupIntentPayment,
} from '../hook/use-price-hooks';
import { IConfirmPlan, IPricePlanWithButton } from '../interface';

type ConfirmSetupHandler = () => Promise<{ status?: string }>;

const SetupIntentPaymentElement = ({
  onReady,
}: {
  onReady: (handler: ConfirmSetupHandler | null) => void;
}) => {
  const stripe = useStripe();
  const elements = useElements();
  const [paymentElementReady, setPaymentElementReady] = useState(false);

  useEffect(() => {
    if (!stripe || !elements || !paymentElementReady) {
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
  }, [elements, onReady, paymentElementReady, stripe]);

  return (
    <PaymentElement
      onReady={() => {
        setPaymentElementReady(true);
      }}
    />
  );
};

const SetupIntentPaymentSection = ({
  clientSecret,
  onReady,
}: {
  clientSecret: string;
  onReady: (handler: ConfirmSetupHandler | null) => void;
}) => {
  const { stripe, isLoading, stripePublishableKey } = useStripeContext();

  return (
    <div className="mb-4 p-4 border border-border-button rounded-md">
      <div className="text-sm text-text-secondary mb-4">
        Please enter your payment details to complete the upgrade
      </div>

      {!stripePublishableKey && (
        <div className="text-sm text-text-secondary p-3 bg-accent-primary-5 rounded-md">
          Payment form is unavailable because Stripe publishable key is not
          configured on the server.
        </div>
      )}

      {stripePublishableKey && isLoading && (
        <div className="text-sm text-text-secondary p-3 bg-accent-primary-5 rounded-md">
          Loading secure payment form...
        </div>
      )}

      {stripePublishableKey && !isLoading && !stripe && (
        <div className="text-sm text-text-secondary p-3 bg-accent-primary-5 rounded-md">
          Failed to load the secure payment form. Please refresh and try again.
        </div>
      )}

      {stripe && (
        <Elements
          key={clientSecret}
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
      )}
    </div>
  );
};

interface ConfirmModalProps {
  plan: IConfirmPlan;
  isOpen: boolean;
  onClose: () => void;
  has_reusable_payment_method?: boolean;
}

export const ConfirmModal: React.FC<ConfirmModalProps> = memo(
  ({ plan, isOpen = true, onClose, has_reusable_payment_method = true }) => {
    const priceDifference = plan.priceDifference;
    const date = getNextMonth.getDayFormatted(getNextMonth.get31Day());
    const price = plan.price;
    const { charge } = useCharge();
    const { createSetupIntent } = useSetupIntentPayment();
    const [loading, setLoading] = useState(false);
    const [showPaymentForm, setShowPaymentForm] = useState(false);
    const [clientSecret, setClientSecret] = useState<string | null>(null);
    const [confirmSetupHandler, setConfirmSetupHandler] =
      useState<ConfirmSetupHandler | null>(null);

    const handleCreateSetupIntent = useCallback(async () => {
      try {
        const result = await createSetupIntent({
          setup_type: 'subscription_upgrade',
          price_id: plan.id,
        });
        setClientSecret(result.client_secret);
        setShowPaymentForm(true);
      } catch (e) {
        message.error((e as Error)?.message || 'Failed to initialize payment');
      }
    }, [createSetupIntent, plan.id]);

    useEffect(() => {
      setShowPaymentForm(false);
      setClientSecret(null);
      setConfirmSetupHandler(null);
    }, [plan.id, isOpen]);

    useEffect(() => {
      if (!isOpen || has_reusable_payment_method || clientSecret || loading) {
        return;
      }

      handleCreateSetupIntent();
    }, [
      clientSecret,
      handleCreateSetupIntent,
      has_reusable_payment_method,
      isOpen,
      loading,
      plan.id,
    ]);

    const handleConfirmWithNewPaymentMethod = async () => {
      if (!confirmSetupHandler) {
        message.error('Payment form is still loading');
        return;
      }

      setLoading(true);
      try {
        localStorage.setItem(
          TrialUpgradeSetupRetryKey,
          JSON.stringify({
            price_id: plan.id,
            quantity: '1',
            payment_type: 'subscription',
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
        localStorage.removeItem(TrialUpgradeSetupRetryKey);
        await charge(plan as unknown as IPricePlanWithButton, {
          setupIntentId,
        });
        onClose();
      } catch (e) {
        localStorage.removeItem(TrialUpgradeSetupRetryKey);
        message.error((e as Error)?.message || 'Failed to confirm payment');
      } finally {
        setLoading(false);
      }
    };

    const onOk = async () => {
      if (!plan?.id) {
        return;
      }

      if (!has_reusable_payment_method && !showPaymentForm) {
        // Need to collect payment method first
        await handleCreateSetupIntent();
        return;
      }

      if (showPaymentForm && clientSecret) {
        // Payment form shown, user needs to complete payment first
        await handleConfirmWithNewPaymentMethod();
        return;
      }

      // Has reusable payment method - proceed with normal charge
      setLoading(true);
      try {
        await charge(plan as unknown as IPricePlanWithButton);
      } catch {
        // charge() shows its own error message
      } finally {
        setLoading(false);
      }
      onClose();
    };

    return (
      <Modal
        open={isOpen}
        onCancel={onClose}
        onOk={onOk}
        title="Change your plan"
        footer={null}
        className=" !w-[600px]"
        confirmLoading={loading}
      >
        <Space direction="vertical">
          <div className="text-sm text-text-primary">
            You are changing to the&nbsp;&nbsp;
            <span className="text-base">{plan.title} Plan. </span>
            Based on your current plan, you need to pay the following prorated
            charge.
          </div>
          <Space align="center">
            <div className="text-text-primary text-sm">Prorated Charge:</div>
            <div className="text-text-primary font-bold text-base">
              ${priceDifference}
            </div>
          </Space>
          <div className="bg-accent-primary-5 rounded-md p-2 flex flex-col gap-2 mb-6 text-text-secondary text-sm">
            <div className="text-xs">Next Billing</div>
            <Space align="center">
              <Space>
                <span>Date:</span>
                <span>{date}</span>
              </Space>
              <Space>
                <span>Amount:</span>
                <span>${price}</span>
              </Space>
            </Space>
          </div>

          {showPaymentForm && clientSecret && (
            <SetupIntentPaymentSection
              clientSecret={clientSecret}
              onReady={(handler) => {
                setConfirmSetupHandler(() => handler);
              }}
            />
          )}

          {!has_reusable_payment_method && !showPaymentForm && (
            <div className="text-sm text-text-secondary mb-4 p-3 bg-accent-primary-5 rounded-md">
              No payment method on file. Initializing secure payment details
              form for this upgrade.
            </div>
          )}
        </Space>
      </Modal>
    );
  },
);

ConfirmModal.displayName = 'ConfirmModal';
