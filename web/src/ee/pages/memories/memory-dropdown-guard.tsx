import { PrivilegeMenuItem } from '@/ee/components/privilege/privilege-dropdown';
import {
  hasManagePermissionPermission,
  hasOwnerPermission,
} from '@/ee/utils/permission-util';
import { IMemory } from '@/pages/memories/interface';
import { MouseEventHandler, ReactNode, useCallback } from 'react';

type MemoryDropdownGuardProps = {
  memory: IMemory;
  showPrivilegeModal(): void;
  manageItems: ReactNode;
  deleteItems: ReactNode;
};

/**
 * Collaborator-aware gating for the memory dropdown. Hides the whole menu
 * when the operator may not manage the memory, and the delete entry unless
 * the operator owns it.
 */
export function MemoryDropdownGuard({
  memory,
  showPrivilegeModal,
  manageItems,
  deleteItems,
}: MemoryDropdownGuardProps) {
  const canManage = hasManagePermissionPermission(
    memory.operator_permission ?? 0,
  );
  const canDelete = hasOwnerPermission(memory.operator_permission ?? 0);

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
