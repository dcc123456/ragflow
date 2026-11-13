import message from '@/components/ui/message';
import { useSetModalState } from '@/hooks/common-hooks';
import { IDataset } from '@/interfaces/database/dataset';
import api from '@/utils/api';
import request from '@/utils/request';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

export default function useDuplicateDataset() {
  const [dataset, setDataset] = useState<IDataset>({} as IDataset);
  const { visible, hideModal, showModal } = useSetModalState();
  const queryClient = useQueryClient();

  const { t } = useTranslation();

  const { mutateAsync: duplicateKnowledge, isPending: loading } = useMutation({
    mutationKey: ['duplicateKnowledge'],
    mutationFn: async (params: { kb_id?: string; name: string }) => {
      const { data = {} } = await request.post(api.duplicate_kb, params);

      if (data.code === 0) {
        message.success(t('message.duplicated'));
        queryClient.invalidateQueries({
          queryKey: ['fetchKnowledgeListByPage'],
        });
      }
      return data;
    },
  });

  const handleShowModal = useCallback(
    (record: IDataset) => {
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
