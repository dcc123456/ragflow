export const openPaymentRedirectPreservingPage = (redirectTo?: string) => {
  if (!redirectTo) return;

  const paymentWindow = window.open(redirectTo, '_blank');
  try {
    if (paymentWindow) paymentWindow.opener = null;
  } catch {
    // Cross-origin browser policies may prevent explicit opener hardening.
  }

  if (paymentWindow) return 'popup' as const;

  window.location.href = redirectTo;
  return 'same_tab' as const;
};
