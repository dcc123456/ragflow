import { isBillingEnabled } from '@/ee/services/billingStatus';
import api from '@/utils/api';

type SilentRule = number | { code: number; messageContains?: string };

/** Error codes to be silenced per API endpoint */
const SilentErrors: Record<string, SilentRule[]> = {
  [api.login]: [{ code: 109, messageContains: 'license' }], // Login endpoint: 109 = token expired, handle silently
};

const shouldSilent = (
  url: string | undefined,
  code: number,
  message?: string,
) => {
  const matchedKey = Object.keys(SilentErrors).find((path) =>
    url?.includes(path),
  );
  if (!matchedKey) return false;
  return SilentErrors[matchedKey].some((rule) => {
    if (typeof rule === 'number') {
      return rule === code;
    }
    const codeMatch = rule.code === code;
    const messageMatch = rule.messageContains
      ? (message?.toLowerCase().includes(rule.messageContains.toLowerCase()) ??
        false)
      : true;
    return codeMatch && messageMatch;
  });
};

const showBillingUpgradeModalIfNeeded = async (data: any) => {
  if (!isBillingEnabled()) {
    return false;
  }

  const { PriceCode, showPriceModal } =
    await import('@/ee/pages/price/global/hook');

  if (!PriceCode[data?.code as keyof typeof PriceCode]) {
    return false;
  }

  showPriceModal(data);
  return true;
};

/**
 * Enterprise response-error handling: opens the billing upgrade modal for
 * billing codes and keeps license-related login errors quiet.
 *
 * Returns `true` when the error was handled here, so the shared interceptor
 * skips its generic notification.
 */
export const handleEnterpriseResponseError = async (
  data: { code?: number; message?: string } | undefined,
  context?: { url?: string },
): Promise<boolean> => {
  if (!data?.code) {
    return false;
  }

  if (await showBillingUpgradeModalIfNeeded(data)) {
    return true;
  }

  return shouldSilent(context?.url, data.code, data.message);
};
