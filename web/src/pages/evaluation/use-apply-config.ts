import { useUpdateChat } from '@/hooks/use-chat-request';
import { useFetchEvaluationRun } from '@/hooks/use-evaluation-request';
import { isEmpty } from 'lodash';
import { useCallback, useMemo } from 'react';
import { UseFormReturn } from 'react-hook-form';
import { useParams } from 'react-router';
import { EvaluationSettingsFormType } from './evaluation-schemas';

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
      params: isEmpty(chat) ? currentDialog : chat,
    });
  }, [currentDialog, form, id, updateChat]);

  return { handleApplyConfig };
}
