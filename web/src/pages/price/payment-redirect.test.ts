import { openPaymentRedirectPreservingPage } from './payment-redirect';

describe('openPaymentRedirectPreservingPage', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('keeps the billing page open when the hosted payment page opens', () => {
    const paymentWindow = { opener: window } as unknown as Window;
    jest.spyOn(window, 'open').mockReturnValue(paymentWindow);
    const close = jest.spyOn(window, 'close').mockImplementation(() => {});

    const result = openPaymentRedirectPreservingPage(
      'https://invoice.stripe.com/test',
    );

    expect(result).toBe('popup');
    expect(paymentWindow.opener).toBeNull();
    expect(close).not.toHaveBeenCalled();
  });

  it('falls back to same-tab navigation when the popup is blocked', () => {
    jest.spyOn(window, 'open').mockReturnValue(null);
    const navigationError = jest
      .spyOn(console, 'error')
      .mockImplementation(() => {});

    const result = openPaymentRedirectPreservingPage(
      'https://invoice.stripe.com/test',
    );

    expect(result).toBe('same_tab');
    expect(navigationError).not.toHaveBeenCalledWith(
      expect.stringContaining('TypeError'),
    );
  });
});
