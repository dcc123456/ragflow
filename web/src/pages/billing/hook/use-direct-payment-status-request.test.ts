import {
  DirectPaymentAttemptEvent,
  DirectPaymentAttemptKey,
  directPaymentRefetchInterval,
  trackDirectPaymentAttempt,
} from './use-direct-payment-status-request';

jest.mock('@/services/price', () => ({
  getDirectSubscriptionPaymentStatus: jest.fn(),
}));

describe('trackDirectPaymentAttempt', () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  it('stores only the identifiers needed for convergence polling', () => {
    const listener = jest.fn();
    window.addEventListener(DirectPaymentAttemptEvent, listener);

    trackDirectPaymentAttempt({
      subscription_id: 'sub-1',
      invoice_id: 'in-1',
      price_id: 'price-1',
      payment_state: 'requires_action',
      redirect_to: 'https://invoice.stripe.com/private',
    });

    expect(
      JSON.parse(sessionStorage.getItem(DirectPaymentAttemptKey)!),
    ).toEqual({
      subscription_id: 'sub-1',
      invoice_id: 'in-1',
      price_id: 'price-1',
    });
    expect(listener).toHaveBeenCalledTimes(1);

    window.removeEventListener(DirectPaymentAttemptEvent, listener);
  });

  it('ignores results without a subscription id', () => {
    trackDirectPaymentAttempt({ invoice_id: 'in-1' });
    expect(sessionStorage.getItem(DirectPaymentAttemptKey)).toBeNull();
  });
});

describe('directPaymentRefetchInterval', () => {
  it.each([
    [undefined, 3000],
    [{ payment_state: 'pending', entitlement_ready: false }, 3000],
    [{ payment_state: 'requires_action', entitlement_ready: false }, 3000],
    [{ payment_state: 'paid', entitlement_ready: false }, 3000],
    [{ payment_state: 'paid', entitlement_ready: true }, false],
    [{ payment_state: 'failed', entitlement_ready: false }, false],
    [{ payment_state: 'scheduled', entitlement_ready: false }, false],
  ])('returns %s for %o', (status, expected) => {
    expect(
      directPaymentRefetchInterval(
        status
          ? {
              subscription_id: 'sub-1',
              ...status,
            }
          : undefined,
      ),
    ).toBe(expected);
  });
});
