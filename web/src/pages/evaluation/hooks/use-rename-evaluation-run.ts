import { useSetModalState } from '@/hooks/common-hooks';
import {
  useFetchEvaluationRun,
  useUpdateEvaluationRun,
} from '@/hooks/use-evaluation-request';
import { IEvaluationRun } from '@/interfaces/database/evaluation';
import { useCallback, useState } from 'react';

export const useRenameEvaluationRun = () => {
  const [run, setRun] = useState<IEvaluationRun | null>(null);
  const {
    visible: renameVisible,
    hideModal: hideRenameModal,
    showModal: showRenameModal,
  } = useSetModalState();
  const { updateEvaluationRun, loading } = useUpdateEvaluationRun();
  const { data: runData } = useFetchEvaluationRun();

  const onRenameOk = useCallback(
    async (name: string) => {
      if (!run && !runData?.id) return;

      const targetRun = run || runData;
      if (!targetRun?.id) return;

      const ret = await updateEvaluationRun({
        runId: targetRun.id,
        name,
        collection_id: targetRun.collection_id,
        config_snapshot: {
          ...targetRun.config_snapshot,
          target: {
            ...targetRun.config_snapshot?.target,
            name,
          },
        },
      });

      if (ret?.code === 0) {
        hideRenameModal();
        setRun(null);
      }
    },
    [run, runData, updateEvaluationRun, hideRenameModal],
  );

  const handleShowRenameModal = useCallback(
    (record?: IEvaluationRun) => {
      if (record) {
        setRun(record);
      } else {
        setRun(null);
      }
      showRenameModal();
    },
    [showRenameModal],
  );

  const handleHideModal = useCallback(() => {
    hideRenameModal();
    setRun(null);
  }, [hideRenameModal]);

  return {
    renameLoading: loading,
    initialRunName: run?.name || runData?.name || '',
    onRenameOk,
    renameVisible,
    hideRenameModal: handleHideModal,
    showRenameModal: handleShowRenameModal,
  };
};
