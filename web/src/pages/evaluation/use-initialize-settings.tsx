import { useFetchDialog } from '@/hooks/use-chat-request';
import { useFetchEvaluationRun } from '@/hooks/use-evaluation-request';
import { useEvaluationUrl } from '@/hooks/use-evaluation-url';
import { isEmpty } from 'lodash';
import { useEffect, useMemo } from 'react';
import { UseFormReturn } from 'react-hook-form';

export function useInitializeSettingsOnMount(form: UseFormReturn<any>) {
  const { data } = useFetchEvaluationRun();
  const { runId } = useEvaluationUrl();

  const { data: dialog } = useFetchDialog();

  const nextData = useMemo(() => {
    if (isEmpty(runId)) {
      return {
        config_snapshot: {
          metircs: {},
          target: dialog,
        },
      };
    }

    return data;
  }, [data, dialog, runId]);

  useEffect(() => {
    if (!isEmpty(nextData)) {
      form.reset(nextData);
    }
  }, [nextData, form]);
}
