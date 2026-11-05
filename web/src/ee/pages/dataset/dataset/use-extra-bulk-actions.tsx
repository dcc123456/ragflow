import { ShowPrivilegeModalReturnType } from '@/ee/pages/dataset/dataset/use-show-privilege-dialog';
import { Key } from 'lucide-react';
import { ReactNode, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

export type BulkOperationAction = {
  id: string;
  label: string;
  icon: ReactNode;
  onClick?: () => void;
};

/**
 * Appends the permission entry to the dataset bulk-operation list so the
 * operator can manage collaborators for the selected documents.
 */
export function useExtraBulkActions(
  handShowPrivilegeModal: ShowPrivilegeModalReturnType['handShowPrivilegeModal'],
  selectedRowKeys: string[],
): BulkOperationAction[] {
  const { t } = useTranslation();

  return useMemo(
    () => [
      {
        id: 'permission',
        label: t('permission.permission'),
        icon: <Key />,
        onClick: handShowPrivilegeModal(selectedRowKeys),
      },
    ],
    [handShowPrivilegeModal, selectedRowKeys, t],
  );
}
