import { PrivilegeMenuItem } from '@/ee/components/privilege/privilege-dropdown';
import {
  hasManagePermissionPermission,
  hasOwnerPermission,
} from '@/ee/utils/permission-util';
import { ISearchAppProps } from '@/pages/next-searches/hooks';
import { MouseEventHandler, ReactNode, useCallback } from 'react';

type SearchDropdownGuardProps = {
  dataset: ISearchAppProps;
  showPrivilegeModal(): void;
  manageItems: ReactNode;
  deleteItems: ReactNode;
};

/**
 * Collaborator-aware gating for the search-app dropdown. Hides the whole menu
 * when the operator may neither manage nor delete the search app, and each
 * group according to the operator's permission.
 */
export function SearchDropdownGuard({
  dataset,
  showPrivilegeModal,
  manageItems,
  deleteItems,
}: SearchDropdownGuardProps) {
  const canManage = hasManagePermissionPermission(
    dataset.operator_permission ?? 0,
  );
  const canDelete = hasOwnerPermission(dataset.operator_permission ?? 0);

  const handleShowPrivilegeModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showPrivilegeModal();
      },
      [showPrivilegeModal],
    );

  if (!canManage && !canDelete) {
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
