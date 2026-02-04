import {
  useCreateRunEvaluation,
  useUpdateEvaluationRun,
} from '@/hooks/use-evaluation-request';
import { useEvaluationUrl } from '@/hooks/use-evaluation-url';
import { useCallback } from 'react';
import { EvaluationSettingsFormType } from './evaluation-schemas';

export function useSubmitSettings() {
  const { createRunEvaluation } = useCreateRunEvaluation();
  const { updateEvaluationRun } = useUpdateEvaluationRun();
  const { runId } = useEvaluationUrl();

  const handleSubmit = useCallback(
    async (data: EvaluationSettingsFormType) => {
      if (runId) {
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

  return { handleSubmit };
}
