import {
  useFetchEnableAdmin,
  useFetchIsAdmin,
} from '@/ee/hooks/use-private-llm-request';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';

/**
 * When the tenant lets an administrator manage model providers, a normal user
 * is told to ask that administrator instead of being sent to a settings page
 * they cannot use. The check waits for both admin flags to settle.
 */
export function useEmptyModelWarning() {
  const { t } = useTranslation();
  const { data: isAdmin, loading: isAdminLoading } = useFetchIsAdmin();
  const { data: enableAdmin, loading: enableAdminLoading } =
    useFetchEnableAdmin();

  const showWarning = useCallback(() => {
    if (!enableAdmin || isAdmin) {
      return false;
    }

    toast.warning(t('setting.requestAdminAddModel'), {
      position: 'top-center',
      closeButton: false,
      duration: 5000,
      id: 'model-providers-warn', // Add a unique ID to prevent duplicate toasts
    });

    return true;
  }, [enableAdmin, isAdmin, t]);

  return {
    isChecking: isAdminLoading || enableAdminLoading,
    showWarning,
  };
}
