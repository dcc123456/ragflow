import { ChatVariableEnabledField } from '@/constants/chat';
import { useFetchChat } from '@/hooks/use-chat-request';
import { useFetchEvaluationRun } from '@/hooks/use-evaluation-request';
import { useEvaluationUrl } from '@/hooks/use-evaluation-url';
import { useFetchDefaultModelDictionary } from '@/hooks/use-llm-request';
import { setLLMSettingEnabledValues } from '@/utils/form';
import { isEmpty } from 'lodash';
import { useEffect, useMemo } from 'react';
import { UseFormReturn } from 'react-hook-form';
import { NewEvaluationRunId } from './constants';

export function useInitializeMetrics() {
  const { llm_id: llmId } = useFetchDefaultModelDictionary();

  const metrics = useMemo(() => {
    const defaultValues = {
      enable: true,
      llm_id: llmId,
    };
    return {
      context_relevance: defaultValues,
      faithfulness: defaultValues,
      semantic_similarity: defaultValues,
    };
  }, [llmId]);

  return {
    metrics,
    llmId,
  };
}

export function useInitializeSettingsOnMount(form: UseFormReturn<any>) {
  const { data } = useFetchEvaluationRun();
  const { runId } = useEvaluationUrl();

  const { data: chat } = useFetchChat();

  const { metrics, llmId } = useInitializeMetrics();

  const nextData = useMemo(() => {
    const isExistingRun = !isEmpty(runId) && runId !== NewEvaluationRunId;

    let baseData: Record<string, any>;
    let target: Record<string, any>;

    if (isExistingRun) {
      baseData = data;
      target = data?.config_snapshot?.target || {};
    } else {
      const values = form.getValues();
      if (
        (runId === NewEvaluationRunId ||
          isEmpty(values.config_snapshot.metrics)) &&
        llmId
      ) {
        baseData = {
          config_snapshot: {
            metrics: metrics,
            target: chat,
          },
          collection_id: '',
        };
        target = chat || {};
      } else {
        return {};
      }
    }

    const computedEnabledValues = setLLMSettingEnabledValues(
      target.llm_setting,
    );
    const enabledValues: Record<string, boolean> = {};
    Object.values(ChatVariableEnabledField).forEach((key) => {
      enabledValues[key] =
        target[key] !== undefined ? target[key] : computedEnabledValues[key];
    });

    return {
      ...enabledValues,
      ...baseData,
      config_snapshot: {
        ...baseData.config_snapshot,
        target: {
          ...enabledValues,
          ...target,
        },
      },
    };
  }, [chat, data, form, llmId, metrics, runId]);

  useEffect(() => {
    if (!isEmpty(nextData)) {
      form.reset(nextData);
    }
  }, [form, nextData]);
}
