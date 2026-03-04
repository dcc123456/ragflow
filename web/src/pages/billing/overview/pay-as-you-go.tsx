import { Button } from '@/components/ui/button';
import { Modal, message } from 'antd';
import { AlertTriangle, FolderCheck, Loader2 } from 'lucide-react';
import { useState } from 'react';
import { PaygStatusEnum } from '../contant';
import {
  useFetchPaygStatus,
  usePaygDisable,
  usePaygEnable,
} from '../hook/overview';

export const PayAsYouGo = () => {
  const { data: paygStatus, loading } = useFetchPaygStatus();
  const enableMutation = usePaygEnable();
  const disableMutation = usePaygDisable();
  const [invoiceModalOpen, setInvoiceModalOpen] = useState(false);
  const [invoiceInfo, setInvoiceInfo] = useState<{
    outstanding_amount: number;
    hosted_invoice_url: string;
    message: string;
  } | null>(null);

  const status = paygStatus?.status ?? PaygStatusEnum.DISABLED;
  const deepdoc = paygStatus?.deepdoc;
  const isActive =
    status === PaygStatusEnum.ACTIVE || status === PaygStatusEnum.GRACE;
  const isCanceling = status === PaygStatusEnum.CANCELING;
  const totalPages = (deepdoc?.pages_paid ?? 0) + (deepdoc?.pages_unpaid ?? 0);
  const totalSpend =
    (deepdoc?.amount_paid ?? 0) + (deepdoc?.amount_unpaid ?? 0);
  const threshold = deepdoc?.threshold ?? 10;

  const paidPercent =
    totalSpend > 0
      ? Math.min(
          ((deepdoc?.amount_paid ?? 0) / Math.max(totalSpend, threshold)) * 100,
          90,
        )
      : 0;
  const unpaidPercent =
    totalSpend > 0
      ? Math.min(
          ((deepdoc?.amount_unpaid ?? 0) / Math.max(totalSpend, threshold)) *
            100,
          90,
        )
      : 0;

  const handleEnable = () => {
    enableMutation.mutate();
  };

  const handleDisableClick = () => {
    disableMutation.mutate(undefined, {
      onSuccess: (res) => {
        const data = res?.data;
        if (!data) return;

        if (data.has_invoice && data.hosted_invoice_url) {
          setInvoiceInfo({
            outstanding_amount: data.outstanding_amount,
            hosted_invoice_url: data.hosted_invoice_url,
            message: data.message,
          });
          setInvoiceModalOpen(true);
        } else if (data.has_invoice && !data.hosted_invoice_url) {
          message.warning(
            'Invoice generated but payment link is unavailable. Please try again later.',
          );
        } else if (data.status === 'disabled') {
          message.success('PAYG has been disabled successfully.');
        }
      },
    });
  };

  if (loading) {
    return (
      <div className="pay-as-you-go flex flex-col justify-between w-full mb-4">
        <h2 className="text-2xl font-bold text-text-primary">Pay-As-You-Go</h2>
        <div className="flex items-center gap-2 mt-4 text-text-secondary">
          <Loader2 className="animate-spin" size={16} />
          Loading...
        </div>
      </div>
    );
  }

  return (
    <div className="pay-as-you-go flex flex-col justify-between w-full mb-4">
      <div className="flex justify-between items-center">
        <h2 className="text-2xl font-bold text-text-primary">Pay-As-You-Go</h2>
        {status === PaygStatusEnum.DISABLED && (
          <Button onClick={handleEnable} disabled={enableMutation.isPending}>
            {enableMutation.isPending ? (
              <>
                <Loader2 className="animate-spin mr-1" size={14} />
                Enabling...
              </>
            ) : (
              'Enable PAYG'
            )}
          </Button>
        )}
        {(isActive || isCanceling) && (
          <Button
            variant="outline"
            onClick={handleDisableClick}
            disabled={disableMutation.isPending}
          >
            {disableMutation.isPending ? (
              <>
                <Loader2 className="animate-spin mr-1" size={14} />
                Processing...
              </>
            ) : isCanceling ? (
              'Pay Outstanding'
            ) : (
              'Disable PAYG'
            )}
          </Button>
        )}
      </div>

      {status === PaygStatusEnum.PENDING && (
        <div className="flex items-center gap-2 mt-4 text-text-secondary">
          <Loader2 className="animate-spin" size={16} />
          Activating your PAYG subscription...
        </div>
      )}

      {isCanceling && (
        <div className="mt-2 flex items-center gap-2 text-yellow-600 bg-yellow-50 border border-yellow-200 p-3 rounded">
          <AlertTriangle size={16} />
          Your subscription is canceled. Please pay the outstanding invoice to
          complete the process.
        </div>
      )}

      {status === PaygStatusEnum.DISABLED && (
        <div className="text-text-secondary mt-2">
          Enable Pay-As-You-Go to use DeepDoc document parsing. We auto-charge
          every time your running total reaches US${threshold}. No spending
          limits—grow as you go.
        </div>
      )}

      {status === PaygStatusEnum.GRACE && (
        <div className="mt-2 flex items-center gap-2 text-yellow-600 bg-yellow-50 border border-yellow-200 p-3 rounded">
          <AlertTriangle size={16} />
          Payment issue detected. Please update your payment method to continue
          using DeepDoc.
        </div>
      )}

      {isActive && (
        <>
          <div className="flex justify-between items-center mt-2">
            <div className="text-text-secondary">
              We auto-charge every time your running total reaches US$
              {threshold}. No spending limits—grow as you go.
            </div>
            {paygStatus?.current_period_start &&
              paygStatus?.current_period_end && (
                <div className="text-text-secondary">
                  Billing cycle: {paygStatus.current_period_start} -{' '}
                  {paygStatus.current_period_end}
                </div>
              )}
          </div>
          <div className="mt-4 bg-bg-input border border-border-default p-4 rounded">
            <p className="text-text-primary text-base flex gap-1 items-center">
              <FolderCheck size={16} />
              Document Parsing {totalPages} pages
            </p>
            <div className="relative mt-3 mb-2 flex items-center w-full border rounded-full border-border-button">
              <div className="h-8 flex items-center rounded-full w-full">
                {paidPercent > 0 && (
                  <div
                    className="h-8 flex items-center bg-accent-primary rounded-l-full pl-3 text-sm"
                    style={{ width: `${Math.max(paidPercent, 15)}%` }}
                  >
                    ${deepdoc?.amount_paid ?? 0} Paid
                  </div>
                )}
                {unpaidPercent > 0 && (
                  <div
                    className="h-8 flex items-center bg-accent-primary rounded-r-full unpaid pl-3 text-black text-sm"
                    style={{ width: `${Math.max(unpaidPercent, 10)}%` }}
                  >
                    ${deepdoc?.amount_unpaid ?? 0}/${threshold} Unpaid
                  </div>
                )}
                <div className="flex flex-1 justify-center pl-7">
                  &#8734; Unlimited
                </div>
              </div>
            </div>
            <div className="flex justify-between items-end">
              <div className="text-text-primary">
                <span className="text-xs">Total spend this cycle: </span>
                <span className="text-base">${totalSpend.toFixed(2)}</span>
              </div>
              <div className="text-text-secondary">
                Any remaining amount under ${threshold} will still be charged at
                the end of your billing cycle
              </div>
            </div>
          </div>
        </>
      )}

      <Modal
        title="Final Invoice"
        open={invoiceModalOpen}
        onCancel={() => setInvoiceModalOpen(false)}
        footer={[
          <Button
            key="close"
            variant="outline"
            onClick={() => setInvoiceModalOpen(false)}
          >
            Close
          </Button>,
          <Button
            key="pay"
            onClick={() => {
              if (invoiceInfo?.hosted_invoice_url) {
                window.open(invoiceInfo.hosted_invoice_url, '_blank');
              }
              setInvoiceModalOpen(false);
            }}
          >
            Go to Pay
          </Button>,
        ]}
      >
        {invoiceInfo && (
          <div className="py-4">
            <p className="mb-4">{invoiceInfo.message}</p>
            <p className="text-lg font-semibold">
              Amount due: ${invoiceInfo.outstanding_amount.toFixed(2)}
            </p>
          </div>
        )}
      </Modal>
    </div>
  );
};
