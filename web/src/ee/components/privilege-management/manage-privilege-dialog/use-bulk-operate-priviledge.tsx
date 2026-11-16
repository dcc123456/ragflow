import { UseRowSelectionType } from '@/hooks/logic-hooks/use-row-selection';
import {
  LabelMap,
  Permission,
  PermissionLevelOptions,
} from '@/ee/constants/team';
import { IPermission } from '@/ee/interfaces/database/team';
import { RowSelectionState } from '@tanstack/react-table';
import { Eye, ShieldCheck, SquarePen, Trash2 } from 'lucide-react';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { IPrivilegeManagementInitialValues } from '@/ee/components/privilege-management/interface';
import { useOperatePermission } from '@/ee/components/privilege-management/manage-privilege-dialog/use-operate-permission';

export function useSelectedRows<T extends Array<{ id: string }>>(
  rowSelection: RowSelectionState,
  list: T,
) {
  const selectedRows = useMemo(() => {
    const indexes = Object.keys(rowSelection);
    return list.filter((_, idx) =>
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
  canGrantManage,
}: Pick<UseRowSelectionType, 'rowSelection' | 'setRowSelection'> & {
  initialValues: IPrivilegeManagementInitialValues;
  permissions: IPermission[];
  canGrantManage: boolean;
}) {
  const { t } = useTranslation();
  const { setPermission } = useOperatePermission({
    initialValues,
  });

  const { selectedRows } = useSelectedRows<IPermission[]>(
    rowSelection,
    permissions,
  );

  const list = [
    ...PermissionLevelOptions.filter(
      ({ action }) => canGrantManage || action !== Permission.Manage,
    ).map(({ action, value }) => ({
      id: LabelMap[action],
      label: t(`permission.${LabelMap[action]}`),
      icon:
        action === Permission.Manage ? (
          <ShieldCheck />
        ) : action === Permission.Write ? (
          <SquarePen />
        ) : (
          <Eye />
        ),
      onClick: async () => {
        await setPermission(value, selectedRows);
        setRowSelection({});
      },
    })),
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
