import { useShowDeleteConfirm } from '@/hooks/common-hooks';
import { useSetDefaultLlm } from '@/hooks/use-private-llm-request';
import { useTranslation } from 'react-i18next';

export const useResetDefaultLLM = (llmFactory: string) => {
  const { setDefaultLlm } = useSetDefaultLlm();
  const showDeleteConfirm = useShowDeleteConfirm();
  const { t } = useTranslation();

  const handleSetDefaultLlm = (name: string) => () => {
    showDeleteConfirm({
      title: t('privateLLM.resetDefaultLLMConfirm'),
      okText: t('common.confirm'),
      onOk: async () => {
        setDefaultLlm({ llm_factory: llmFactory, llm_name: name });
      },
    });
  };

  return { handleSetDefaultLlm };
};
