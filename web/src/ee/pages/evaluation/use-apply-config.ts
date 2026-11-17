import { useUpdateChat } from '@/hooks/use-chat-request';
import {
  evaluationSnapshot,
  useFetchEvaluationRun,
} from '@/ee/hooks/use-evaluation-request';
import { isEmpty } from 'lodash';
import { useCallback, useMemo } from 'react';
import { UseFormReturn } from 'react-hook-form';
import { useParams } from 'react-router';
import { EvaluationSettingsFormType } from '@/ee/pages/evaluation/evaluation-schemas';

export function useApplyConfig(
  form: UseFormReturn<EvaluationSettingsFormType>,
) {
  const { updateChat } = useUpdateChat();
  const { id } = useParams();
  const { data } = useFetchEvaluationRun();

  const currentDialog = useMemo(
    () => data.config_snapshot?.target ?? {},
    [data],
  );

  const handleApplyConfig = useCallback(() => {
    if (!id) {
      return;
    }

    const data = form.getValues();
    const chat = data.config_snapshot.target ?? [];

    updateChat({
      chatId: id,
      params: evaluationSnapshot({
        target: isEmpty(chat) ? currentDialog : chat,
      })!.target,
    });
  }, [currentDialog, form, id, updateChat]);

  return { handleApplyConfig };
}
