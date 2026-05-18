import { Button } from '@/components/ui/button';
import { Modal } from '@/components/ui/modal/modal';
import {
  BillingDirectCheckoutResultEvent,
  TrialUpgradeSetupRetryResultKey,
} from '@/pages/price/hook/use-price-hooks';
import { CheckCircle, Loader2, XCircle } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PaymentStatus, PaymentStatusMap } from '../constants/payment-status';
import {
  SessionData,
  useFetchPaymentSession,
} from '../hook/use-payment-status-request';

const formatCurrencyAmount = (amount?: number, currency?: string) => {
  if (amount === undefined) {
    return '';
  }

  const normalizedCurrency = (currency || 'USD').toUpperCase();

  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: normalizedCurrency,
    }).format(amount);
  } catch {
    return `${normalizedCurrency} ${amount.toLocaleString()}`;
  }
};

const PaymentStatusModal: React.FC = () => {
  const { t } = useTranslation();
  const [status, setStatus] = useState<PaymentStatus | null>(null);
  const [sessionData, setSessionData] = useState<SessionData | null>(null);

  const urlParams = useMemo(
    () => new URLSearchParams(window.location.search),
    [],
  );
  const payStatus = urlParams.get('price-pay-status');
  const sessionId = urlParams.get('session_id');

  const shouldPoll = status === PaymentStatus.Pending && !!sessionId;

  const { data: pollingData, isError } = useFetchPaymentSession(
    sessionId,
    shouldPoll,
  );

  useEffect(() => {
    if (payStatus && sessionId) {
      setStatus(PaymentStatus.Pending);
      return;
    }

    const retryResult = sessionStorage.getItem(TrialUpgradeSetupRetryResultKey);
    if (!retryResult) {
      return;
    }

    try {
      setSessionData(JSON.parse(retryResult) as SessionData);
      setStatus(PaymentStatus.Success);
    } catch {
      sessionStorage.removeItem(TrialUpgradeSetupRetryResultKey);
    }
  }, [payStatus, sessionId]);

  useEffect(() => {
    const handleDirectCheckoutResult = (event: Event) => {
      const customEvent = event as CustomEvent<SessionData>;
      if (!customEvent.detail) {
        return;
      }
      setSessionData(customEvent.detail);
      setStatus(PaymentStatus.Success);
    };

    window.addEventListener(
      BillingDirectCheckoutResultEvent,
      handleDirectCheckoutResult as EventListener,
    );
    return () => {
      window.removeEventListener(
        BillingDirectCheckoutResultEvent,
        handleDirectCheckoutResult as EventListener,
      );
    };
  }, []);

  useEffect(() => {
    if (!pollingData) return;

    setSessionData(pollingData);
    setStatus(PaymentStatusMap[pollingData.status]);
  }, [pollingData]);

  useEffect(() => {
    if (isError) {
      setStatus(PaymentStatus.Failed);
    }
  }, [isError]);

  useEffect(() => {
    if (status === PaymentStatus.Success || status === PaymentStatus.Failed) {
      const urlObj = new URL(window.location.href);
      urlObj.searchParams.delete('price-pay-status');
      urlObj.searchParams.delete('session_id');
      window.history.replaceState({}, '', urlObj.toString());
      sessionStorage.removeItem(TrialUpgradeSetupRetryResultKey);
    }
  }, [status]);

  const handleClose = () => {
    setStatus(null);
    setSessionData(null);
  };

  const isOpen = status !== null;

  const icon = useMemo(() => {
    switch (status) {
      case PaymentStatus.Pending:
        return <Loader2 className="w-12 h-12 text-cyan-400 animate-spin" />;
      case PaymentStatus.Success:
        return <CheckCircle className="w-12 h-12 text-green-500" />;
      case PaymentStatus.Failed:
        return <XCircle className="w-12 h-12 text-red-500" />;
      default:
        return null;
    }
  }, [status]);

  const title = useMemo(() => {
    switch (status) {
      case PaymentStatus.Pending:
        return t('billing.paymentPending');
      case PaymentStatus.Success:
        return t('billing.paymentSuccess');
      case PaymentStatus.Failed:
        return t('billing.paymentFailed');
      default:
        return '';
    }
  }, [status, t]);

  if (!isOpen) return null;

  return (
    <Modal
      open={isOpen}
      onOpenChange={(open) => !open && handleClose()}
      title={null}
      className="!w-[400px]"
      footer={
        <div className="flex justify-end">
          <Button variant="outline" onClick={handleClose}>
            {t('billing.close')}
          </Button>
        </div>
      }
    >
      <div className="flex flex-col items-center py-4">
        <div className="mb-4">{icon}</div>
        <h3 className="text-lg font-medium text-text-primary mb-6">{title}</h3>

        <div className="w-full bg-bg-input rounded-lg p-4 space-y-3">
          {sessionData?.amount !== undefined && (
            <div className="flex justify-between items-center">
              <span className="text-sm text-text-secondary">
                {t('billing.amount')}
              </span>
              <span className="text-sm font-medium text-cyan-400">
                {formatCurrencyAmount(sessionData.amount, sessionData.currency)}
              </span>
            </div>
          )}
          {sessionData?.credits !== undefined && (
            <div className="flex justify-between items-center">
              <span className="text-sm text-text-secondary">
                {t('billing.points')}
              </span>
              <span className="text-sm font-medium text-cyan-400">
                {sessionData.credits.toLocaleString()} {t('billing.points')}
              </span>
            </div>
          )}
          {sessionData?.invoice_id && (
            <div className="flex justify-between items-center">
              <span className="text-sm text-text-secondary">
                {t('billing.invoiceID')}
              </span>
              <span className="text-sm font-medium text-cyan-400">
                {sessionData.invoice_id}
              </span>
            </div>
          )}
          {sessionData?.subscription_id && (
            <div className="flex justify-between items-center">
              <span className="text-sm text-text-secondary">
                {t('billing.subscriptionID')}
              </span>
              <span className="text-sm font-medium text-cyan-400">
                {sessionData.subscription_id}
              </span>
            </div>
          )}
          {sessionData?.plan_name && (
            <div className="flex justify-between items-center">
              <span className="text-sm text-text-secondary">
                {t('billing.plan')}
              </span>
              <span className="text-sm font-medium text-cyan-400">
                {sessionData.plan_name}
              </span>
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
};

export default PaymentStatusModal;
