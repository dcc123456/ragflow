import { IEvaluationRunResult } from '@/interfaces/database/evaluation';
import { useCallback, useState } from 'react';
import { useSetModalState } from './common-hooks';

export const useMetricsDetailDialog = (results?: {
  results?: IEvaluationRunResult[];
}) => {
  const {
    visible: detailVisible,
    hideModal: hideDetailModal,
    showModal: showDetailModal,
  } = useSetModalState();

  const [selectedResult, setSelectedResult] = useState<IEvaluationRunResult>();

  const handleShowDetail = useCallback(
    (caseId: string) => {
      const fullResult = results?.results?.find((r) => r.case_id === caseId);
      setSelectedResult(fullResult);
      showDetailModal();
    },
    [results, showDetailModal],
  );

  return {
    detailVisible,
    hideDetailModal,
    handleShowDetail,
    selectedResult,
  };
};
