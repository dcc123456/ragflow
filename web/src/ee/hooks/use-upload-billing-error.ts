import { showPriceModal } from '@/ee/pages/price/global/hook';
import { useCallback } from 'react';

export type UploadBillingResponse = {
  code?: number;
  message?: string;
  detail?: { current?: number; limit?: number; [key: string]: any };
};

/**
 * File uploads go through umi-request, whose global interceptor already opens
 * the upgrade modal for billing codes 2000-2005. Codes 2006/2007 are not
 * covered there, so the file-management page dispatches the modal itself to
 * match the dataset upload behavior.
 */
export function useUploadBillingError() {
  const handleUploadError = useCallback((ret?: UploadBillingResponse) => {
    if (
      !ret ||
      typeof ret.code !== 'number' ||
      ret.code < 2000 ||
      ret.code > 2007
    ) {
      return false;
    }

    showPriceModal({
      code: ret.code,
      detail: ret.detail,
      message: ret.message,
    });

    return true;
  }, []);

  return { handleUploadError };
}
