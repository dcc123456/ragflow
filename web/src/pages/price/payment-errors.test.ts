import { billingPaymentErrorTranslationKey } from './payment-errors';

describe('billingPaymentErrorTranslationKey', () => {
  it('uses the duplicate-subscription support message', () => {
    expect(billingPaymentErrorTranslationKey('duplicate_subscription')).toBe(
      'price.duplicateSubscription',
    );
  });

  it('does not expose unknown backend error text', () => {
    expect(billingPaymentErrorTranslationKey('unknown_provider_error')).toBe(
      'price.paymentFailedTip',
    );
  });
});
