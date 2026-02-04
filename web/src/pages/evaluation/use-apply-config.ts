import { useSetDialog } from '@/hooks/use-chat-request';
import { useFetchEvaluationRun } from '@/hooks/use-evaluation-request';
import { isEmpty } from 'lodash';
import { useCallback, useMemo } from 'react';
import { UseFormReturn } from 'react-hook-form';
import { useParams } from 'react-router';
import { EvaluationSettingsFormType } from './evaluation-schemas';

export function useApplyConfig(
  form: UseFormReturn<EvaluationSettingsFormType>,
) {
  const { setDialog } = useSetDialog();
  const { id } = useParams();
  const { data } = useFetchEvaluationRun();

  const currentDialog = useMemo(
    () => data.config_snapshot?.target ?? {},
    [data],
  );

  const handleApplyConfig = useCallback(() => {
    const data = form.getValues();
    const dialog = data.config_snapshot.target ?? [];
    setDialog({
      ...(isEmpty(dialog) ? currentDialog : dialog),
      dialog_id: id,
    });
  }, [currentDialog, form, id, setDialog]);

  return { handleApplyConfig };
}
