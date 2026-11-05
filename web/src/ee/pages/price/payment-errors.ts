const BillingPaymentErrorTranslationKeys: Record<string, string> = {
  duplicate_subscription: 'price.duplicateSubscription',
  payment_action_required: 'price.paymentVerificationRequired',
  payment_declined: 'price.paymentDeclined',
  payment_service_unavailable: 'price.paymentServiceUnavailable',
  payment_failed: 'price.paymentFailedTip',
};

export const billingPaymentErrorTranslationKey = (errorType?: string) =>
  BillingPaymentErrorTranslationKeys[errorType || ''] ||
  'price.paymentFailedTip';
