import { OnChangeFn, RowSelectionState } from '@tanstack/react-table';
import { Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';

export function useBulkOperateEvaluation({
  rowSelection,
  setRowSelection,
  deleteCallBack,
}: {
  rowSelection: RowSelectionState;
  setRowSelection: OnChangeFn<RowSelectionState>;
  deleteCallBack: (collectionIds: string[]) => Promise<void>;
}) {
  const { t } = useTranslation();

  const selectedIds = Object.keys(rowSelection).filter(
    (id) => rowSelection[id],
  );

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
