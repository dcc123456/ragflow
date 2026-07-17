import request from '@/utils/request';
import { billingCheckout, getDirectSubscriptionPaymentStatus } from '../price';

jest.mock('@/utils/request', () => ({
  __esModule: true,
  default: {
    get: jest.fn(),
    post: jest.fn(),
  },
}));

jest.mock('@/utils/register-server', () => ({
  __esModule: true,
  default: jest.fn(() => ({})),
}));

describe('billing price service', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('makes billing checkout the single owner of error presentation', () => {
    billingCheckout({
      tenant_id: 'tenant-1',
      subscription_price_id: 'price-1',
      payment_type: 'subscription',
      quantity: '1',
      session_cancel_url: 'https://app.example/cancel',
      session_success_url: 'https://app.example/success',
    });

    expect(request.post).toHaveBeenCalledWith(
      expect.stringContaining('/billing/subscription'),
      expect.objectContaining({ noToast: true }),
    );
  });

  it('requests direct status with subscription and invoice identifiers', () => {
    getDirectSubscriptionPaymentStatus('sub-1', 'in-1');

    expect(request.get).toHaveBeenCalledWith(
      expect.stringContaining('/billing/subscriptions/sub-1/payment-status'),
      { params: { invoice_id: 'in-1' }, noToast: true },
    );
  });
});
