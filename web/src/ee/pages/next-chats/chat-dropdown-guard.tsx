import { PrivilegeMenuItem } from '@/ee/components/privilege/privilege-dropdown';
import {
  hasManagePermissionPermission,
  hasOwnerPermission,
} from '@/ee/utils/permission-util';
import { IDialog } from '@/interfaces/database/chat';
import { MouseEventHandler, ReactNode, useCallback } from 'react';

type ChatDropdownGuardProps = {
  chat: IDialog;
  showPrivilegeModal(): void;
  manageItems: ReactNode;
  deleteItems: ReactNode;
};

/**
 * Collaborator-aware gating for the chat dropdown. Hides the whole menu when
 * the operator may not manage the chat, and the delete entry unless the
 * operator owns it.
 */
export function ChatDropdownGuard({
  chat,
  showPrivilegeModal,
  manageItems,
  deleteItems,
}: ChatDropdownGuardProps) {
  const canManage = hasManagePermissionPermission(chat.operator_permission);
  const canDelete = hasOwnerPermission(chat.operator_permission);

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
