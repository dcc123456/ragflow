import { Button } from '@/components/ui/button';
import { ShowPrivilegeModalContext } from '@/ee/pages/dataset/dataset/use-show-privilege-dialog';
import { IDocumentInfo } from '@/interfaces/database/document';
import { Key } from 'lucide-react';
import { useContext } from 'react';
import { useTranslation } from 'react-i18next';

export type DocumentActionCellOverrides = {
  actionClassName?: string;
};

export type DocumentPrivilegeActionProps = {
  record: IDocumentInfo;
  disabled: boolean;
};

/**
 * Row actions stay visible for collaborators, sit slightly wider apart and
 * expose the permission dialog entry.
 */
const overrides: DocumentActionCellOverrides = {
  actionClassName: 'gap-4 opacity-100 text-text-sub-title-invert',
};

export function useDocumentActionCellOverrides(): DocumentActionCellOverrides {
  return overrides;
}

export function DocumentPrivilegeAction({
  record,
  disabled,
}: DocumentPrivilegeActionProps) {
  const { t } = useTranslation();
  const { handShowPrivilegeModal } = useContext(ShowPrivilegeModalContext);

  return (
    <Button
      variant="ghost"
      size="sm"
      disabled={disabled}
      aria-label={t('permission.permissionManagement')}
      onClick={handShowPrivilegeModal?.(record)}
    >
      <Key />
    </Button>
  );
}
