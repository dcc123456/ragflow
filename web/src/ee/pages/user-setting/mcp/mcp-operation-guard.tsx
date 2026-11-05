import { PrivilegeMenuItem } from '@/ee/components/privilege/privilege-dropdown';
import {
  hasManagePermissionPermission,
  hasOwnerPermission,
  hasPreviewPermission,
} from '@/ee/utils/permission-util';
import { IMcpServer } from '@/interfaces/database/mcp';
import { MouseEventHandler, ReactNode, useCallback } from 'react';

type McpOperationGuardProps = {
  mcp: IMcpServer;
  showPrivilegeModal(): void;
  editItems: ReactNode;
  viewItems: ReactNode;
  ownerItems: ReactNode;
};

/**
 * Collaborator-aware gating for the MCP server action menu. Shows the view
 * entry for preview-only operators, the privilege entry for managers and the
 * export/delete group for owners.
 */
export function McpOperationGuard({
  mcp,
  showPrivilegeModal,
  editItems,
  viewItems,
  ownerItems,
}: McpOperationGuardProps) {
  const isReadOnly = hasPreviewPermission(mcp.operator_permission);
  const canManage = hasManagePermissionPermission(mcp.operator_permission);
  const canDelete = hasOwnerPermission(mcp.operator_permission);

  const handleShowPrivilegeModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showPrivilegeModal();
      },
      [showPrivilegeModal],
    );

  return (
    <>
      {isReadOnly ? viewItems : editItems}
      {canManage && <PrivilegeMenuItem onClick={handleShowPrivilegeModal} />}
      {canDelete && <>{ownerItems}</>}
    </>
  );
}
