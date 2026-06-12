import { useFetchChat } from '@/hooks/use-chat-request';
import { useFetchEvaluationRun } from '@/hooks/use-evaluation-request';
import { useEvaluationUrl } from '@/hooks/use-evaluation-url';
import { useFetchDefaultModelDictionary } from '@/hooks/use-llm-request';
import { isEmpty } from 'lodash';
import { useEffect, useMemo } from 'react';
import { UseFormReturn } from 'react-hook-form';

export function useInitializeMetrics() {
  const { llm_id: llmId } = useFetchDefaultModelDictionary();

  const defaultValues = {
    enable: true,
    llm_id: llmId,
  };

  return {
    metrics: {
      context_relevance: defaultValues,
      faithfulness: defaultValues,
      semantic_similarity: defaultValues,
    },
    llmId,
  };
}

export function useInitializeSettingsOnMount(form: UseFormReturn<any>) {
  const { data } = useFetchEvaluationRun();
  const { runId } = useEvaluationUrl();

  const { data: chat } = useFetchChat();

  const { metrics, llmId } = useInitializeMetrics();

  const nextData = useMemo(() => {
    if (!isEmpty(runId)) {
      return data;
    }

    const values = form.getValues();
    if (isEmpty(values.config_snapshot.metrics) && llmId) {
      return {
        config_snapshot: {
          metrics: metrics,
          target: chat,
        },
        collection_id: '',
      };
    }

    return {};
  }, [chat, data, form, llmId, metrics, runId]);

  useEffect(() => {
    if (!isEmpty(nextData)) {
      form.reset(nextData);
    }
  }, [form, nextData]);
}
