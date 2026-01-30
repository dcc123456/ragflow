import { useSelectedIds } from '@/hooks/logic-hooks/use-row-selection';
import { IEvaluationCollection } from '@/interfaces/database/evaluation';
import { OnChangeFn, RowSelectionState } from '@tanstack/react-table';
import { Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';

export function useBulkOperateEvaluation({
  evaluationData,
  rowSelection,
  setRowSelection,
  deleteCallBack,
}: {
  evaluationData: IEvaluationCollection[];
  rowSelection: RowSelectionState;
  setRowSelection: OnChangeFn<RowSelectionState>;
  deleteCallBack: (collectionIds: string[]) => Promise<void>;
}) {
  const { t } = useTranslation();

  const { selectedIds } = useSelectedIds(rowSelection, evaluationData);

  // const { handleRemoveFile } = useHandleDeleteFile();

  const list = [
    {
      id: 'delete',
      label: t('common.delete'),
      icon: <Trash2 />,
      onClick: async () => {
        console.log('Deleting selected items:', selectedIds);
        await deleteCallBack(selectedIds);
        setRowSelection({});
      },
    },
  ];

  return { list };
}
