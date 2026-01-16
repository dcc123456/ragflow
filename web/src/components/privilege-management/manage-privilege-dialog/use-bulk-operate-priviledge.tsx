import { UseRowSelectionType } from '@/hooks/logic-hooks/use-row-selection';
import { IPermission } from '@/interfaces/database/team';
import { RowSelectionState } from '@tanstack/react-table';
import { Ban, CircleX, Play, Trash2 } from 'lucide-react';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { IPrivilegeManagementInitialValues } from '../interface';
import { useOperatePermission } from './use-operate-permission';

export function useSelectedRows<T extends Array<{ id: string }>>(
  rowSelection: RowSelectionState,
  list: T,
) {
  const selectedRows = useMemo(() => {
    const indexes = Object.keys(rowSelection);
    return list.filter((x, idx) =>
      indexes.some((y) => Number(y) === idx),
    ) as IPermission[];
  }, [list, rowSelection]);

  return { selectedRows };
}

export function useBulkOperatePrivilege({
  rowSelection,
  setRowSelection,
  initialValues,
  permissions,
}: Pick<UseRowSelectionType, 'rowSelection' | 'setRowSelection'> & {
  initialValues: IPrivilegeManagementInitialValues;
  permissions: IPermission[];
}) {
  const { t } = useTranslation();
  const { handleSwitchPermission, setPermission } = useOperatePermission({
    initialValues,
  });

  const { selectedRows } = useSelectedRows<IPermission[]>(
    rowSelection,
    permissions,
  );

  const list = [
    {
      id: 'read',
      label: t('permission.readPermission'),
      icon: <Ban />,
      onClick: () => {
        handleSwitchPermission('1', selectedRows);
      },
    },
    {
      id: 'write',
      label: t('permission.writePermission'),
      icon: <Play />,
      onClick: () => {
        handleSwitchPermission('2', selectedRows);
      },
    },
    {
      id: 'manage',
      label: t('permission.managePermission'),
      icon: <CircleX />,
      onClick: () => {
        handleSwitchPermission('4', selectedRows);
      },
    },
    {
      id: 'delete',
      label: t('common.delete'),
      icon: <Trash2 />,
      onClick: async () => {
        await setPermission(0, selectedRows);
        setRowSelection({});
      },
    },
  ];

  return { list };
}
