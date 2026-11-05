import {
  useCreateRunEvaluation,
  useUpdateEvaluationRun,
} from '@/ee/hooks/use-evaluation-request';
import { useEvaluationUrl } from '@/ee/hooks/use-evaluation-url';
import { useCallback } from 'react';
import { NewEvaluationRunId } from '@/ee/pages/evaluation/constants';
import { EvaluationSettingsFormType } from '@/ee/pages/evaluation/evaluation-schemas';

export function useSubmitSettings() {
  const { createRunEvaluation, loading } = useCreateRunEvaluation();
  const { updateEvaluationRun, loading: updateLoading } =
    useUpdateEvaluationRun();
  const { runId } = useEvaluationUrl();

  const handleSubmit = useCallback(
    async (data: EvaluationSettingsFormType) => {
      if (runId && runId !== NewEvaluationRunId) {
        updateEvaluationRun({
          runId,
          collection_id: data.collection_id,
          config_snapshot: data.config_snapshot,
        });
      } else {
        createRunEvaluation({
          collection_id: data.collection_id,
          config_snapshot: data.config_snapshot,
        });
      }
    },
    [createRunEvaluation, runId, updateEvaluationRun],
  );

  return { handleSubmit, loading: loading || updateLoading };
}
