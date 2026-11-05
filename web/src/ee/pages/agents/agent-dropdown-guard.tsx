import { PrivilegeMenuItem } from '@/ee/components/privilege/privilege-dropdown';
import {
  hasManagePermissionPermission,
  hasOwnerPermission,
  showEditButton,
} from '@/ee/utils/permission-util';
import { IFlow } from '@/interfaces/database/agent';
import { MouseEventHandler, ReactNode, useCallback } from 'react';

type AgentDropdownGuardProps = {
  agent: IFlow;
  showPrivilegeModal(): void;
  manageItems: ReactNode;
  deleteItems: ReactNode;
};

/**
 * Collaborator-aware gating for the agent dropdown. Hides the whole menu when
 * the operator has no edit permission, and each group when the operator may
 * not manage or delete the agent.
 */
export function AgentDropdownGuard({
  agent,
  showPrivilegeModal,
  manageItems,
  deleteItems,
}: AgentDropdownGuardProps) {
  const canManage = hasManagePermissionPermission(agent.operator_permission);
  const canDelete = hasOwnerPermission(agent.operator_permission);

  const handleShowPrivilegeModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showPrivilegeModal();
      },
      [showPrivilegeModal],
    );

  if (!showEditButton(agent.operator_permission)) {
    return null;
  }

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
