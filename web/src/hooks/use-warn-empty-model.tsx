import { Modal } from '@/components/ui/modal/modal';
import DOMPurify from 'dompurify';
import { isEmpty } from 'lodash';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { useNavigatePage } from './logic-hooks/navigate-hooks';
import {
  useFetchEnableAdmin,
  useFetchIsAdmin,
} from './use-private-llm-request';

export const useWarnEmptyModel = (
  showEmptyModelWarn: boolean,
  embdId?: string,
  llmId?: string,
  loading?: boolean,
) => {
  const { t } = useTranslation();
  const { navigateToModelSetting } = useNavigatePage();
  const { data: isAdmin, loading: isAdminLoading } = useFetchIsAdmin();
  const { data: enableAdmin, loading: enableAdminLoading } =
    useFetchEnableAdmin();

  useEffect(() => {
    if (
      !isAdminLoading &&
      !enableAdminLoading &&
      !loading &&
      (isEmpty(embdId) || isEmpty(llmId)) &&
      typeof embdId === 'string' &&
      typeof llmId === 'string' &&
      showEmptyModelWarn
    ) {
      if (enableAdmin && !isAdmin) {
        toast.warning(t('setting.requestAdminAddModel'), {
          position: 'top-center',
          closeButton: false,
          duration: 5000,
          id: 'model-providers-warn', // Add a unique ID to prevent duplicate toasts
        });
      } else {
        Modal.warning({
          title: t('common.warn'),
          content: (
            <div
              dangerouslySetInnerHTML={{
                __html: DOMPurify.sanitize(t('setting.modelProvidersWarn')),
              }}
            ></div>
          ),
          closable: false,
          showCancel: false,
          onOk() {
            navigateToModelSetting();
          },
        });
      }
    }
  }, [
    showEmptyModelWarn,
    embdId,
    llmId,
    loading,
    navigateToModelSetting,
    t,
    isAdmin,
    enableAdmin,
    isAdminLoading,
    enableAdminLoading,
  ]);
};
