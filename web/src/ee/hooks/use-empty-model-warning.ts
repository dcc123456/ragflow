import { useFetchExposeModelProvider } from '@/ee/hooks/use-private-llm-request';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';

/**
 * When model-provider settings are hidden, users are told to ask their
 * administrator instead of being sent to that settings page.
 */
export function useEmptyModelWarning() {
  const { t } = useTranslation();
  const { data: exposeModelProvider, loading } = useFetchExposeModelProvider();

  const showWarning = useCallback(() => {
    if (exposeModelProvider) {
      return false;
    }

    toast.warning(t('setting.requestAdminAddModel'), {
      position: 'top-center',
      closeButton: false,
      duration: 5000,
      id: 'model-providers-warn', // Add a unique ID to prevent duplicate toasts
    });

    return true;
  }, [exposeModelProvider, t]);

  return {
    isChecking: loading,
    showWarning,
  };
}
