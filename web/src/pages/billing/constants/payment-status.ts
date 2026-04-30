export type StripePaymentStatus =
  | 'paid'
  | 'unpaid'
  | 'no_payment_required'
  | 'unknown';

export enum PaymentStatus {
  Pending = 'pending',
  Success = 'success',
  Failed = 'failed',
}

export const PaymentStatusMap: Record<StripePaymentStatus, PaymentStatus> = {
  paid: PaymentStatus.Success,
  no_payment_required: PaymentStatus.Success,
  unpaid: PaymentStatus.Failed,
  unknown: PaymentStatus.Failed,
};

export const isPaymentSuccess = (status: StripePaymentStatus): boolean =>
  status === 'paid' || status === 'no_payment_required';
