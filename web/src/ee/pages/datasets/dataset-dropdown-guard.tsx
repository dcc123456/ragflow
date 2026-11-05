import { PrivilegeMenuItem } from '@/ee/components/privilege/privilege-dropdown';
import {
  hasManagePermissionPermission,
  hasOwnerPermission,
} from '@/ee/utils/permission-util';
import { IDataset } from '@/interfaces/database/dataset';
import { MouseEventHandler, ReactNode, useCallback } from 'react';

type DatasetDropdownGuardProps = {
  dataset: IDataset;
  showPrivilegeModal(): void;
  manageItems: ReactNode;
  deleteItems: ReactNode;
};

/**
 * Collaborator-aware gating for the dataset dropdown. Hides the whole menu
 * when the operator may not manage the dataset, and the delete entry unless
 * the operator owns it.
 */
export function DatasetDropdownGuard({
  dataset,
  showPrivilegeModal,
  manageItems,
  deleteItems,
}: DatasetDropdownGuardProps) {
  const canManage = hasManagePermissionPermission(dataset.operator_permission);
  const canDelete = hasOwnerPermission(dataset.operator_permission);

  const handleShowPrivilegeModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showPrivilegeModal();
      },
      [showPrivilegeModal],
    );

  if (!canManage) {
    return null;
  }

  return (
    <>
      {canManage && (
        <>
          {manageItems}
          <PrivilegeMenuItem onClick={handleShowPrivilegeModal} />
        </>
      )}
      {canDelete && <>{deleteItems}</>}
    </>
  );
}
