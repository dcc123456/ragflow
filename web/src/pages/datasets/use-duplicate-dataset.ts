import { useSetModalState } from '@/hooks/common-hooks';
import { useDuplicateKnowledge } from '@/hooks/use-knowledge-request';
import type { IKnowledge } from '@/interfaces/database/knowledge';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

export default function useDuplicateDataset() {
  const [dataset, setDataset] = useState<IKnowledge>({} as IKnowledge);
  const { visible, hideModal, showModal } = useSetModalState();

  const { t } = useTranslation();
  const { duplicateKnowledge, loading } = useDuplicateKnowledge(true);

  const handleShowModal = useCallback(
    (record: IKnowledge) => {
      setDataset(record);
      showModal();
    },
    [showModal],
  );

  const onOk = useCallback(
    async (name: string) => {
      const ret = await duplicateKnowledge({
        name,
        kb_id: dataset.id,
      });

      if (ret.code === 0) {
        hideModal();
      }
    },
    [duplicateKnowledge, dataset, hideModal],
  );

  const initialName = useMemo(() => {
    return dataset?.name
      ? `${dataset.name} ${t('common.duplicateSuffix')}`
      : '';
  }, [dataset.name, t]);

  return {
    isModalVisible: visible,
    showModal: handleShowModal,
    hideModal,

    initialName,

    onOk,
    loading,
  };
}
