import { Button } from '@/components/ui/button';
import { Permission, hasResourcePermission } from '@/ee/constants/team';
import { IFile } from '@/interfaces/database/file-manager';
import { isKnowledgeBaseType } from '@/pages/files/util';
import { KeyRound } from 'lucide-react';
import { useTranslation } from 'react-i18next';

type FilePrivilegeButtonProps = {
  record: IFile;
  showPrivilegeModal?(record: IFile): void;
};

/**
 * Enterprise permission entry point for a file or folder row. Only managers
 * and owners of the resource can open the collaborator dialog, so the button
 * stays hidden without a handler or without sufficient permission.
 */
export function FilePrivilegeButton({
  record,
  showPrivilegeModal,
}: FilePrivilegeButtonProps) {
  const { t } = useTranslation();
  const operatorPermission = record.operator_permission ?? 0;
  const canManagePermissions =
    hasResourcePermission(operatorPermission, Permission.Manage) ||
    hasResourcePermission(operatorPermission, Permission.Owner);

  if (
    !showPrivilegeModal ||
    isKnowledgeBaseType(record.source_type) ||
    !canManagePermissions
  ) {
    return null;
  }

  return (
    <Button
      variant="transparent"
      className="border-none hover:bg-bg-card text-text-primary"
      size="icon-sm"
      aria-label={t('permission.permissionManagement')}
      onClick={() => showPrivilegeModal(record)}
    >
      <KeyRound />
    </Button>
  );
}
