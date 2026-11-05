import {
  hasOwnerPermission,
  hasPreviewPermission,
} from '@/ee/utils/permission-util';
import { IMcpServer } from '@/interfaces/database/mcp';
import { ReactNode } from 'react';

type McpToolbarGuardProps = {
  mcpList: IMcpServer[];
  bulkManageButton: ReactNode;
  children: ReactNode;
};

type McpSelectionBarGuardProps = {
  mcpList: IMcpServer[];
  children: ReactNode;
};

type McpEmptyStateGuardProps = {
  mcpList: IMcpServer[];
  addButton: ReactNode;
  onClickAdd(): void;
  children: ReactNode;
};

function hasAnyWritePermission(mcpList: IMcpServer[]) {
  return (
    mcpList?.length === 0 ||
    Boolean(
      mcpList?.some((item) => !hasPreviewPermission(item.operator_permission)),
    )
  );
}

function hasAnyOwnerPermission(mcpList: IMcpServer[]) {
  return Boolean(
    mcpList?.some((item) => hasOwnerPermission(item.operator_permission)),
  );
}

/**
 * Collaborator-aware gating for the MCP server list. The toolbar and the empty
 * state need write access, and bulk management needs an owned server.
 */
export function McpToolbarGuard({
  mcpList,
  bulkManageButton,
  children,
}: McpToolbarGuardProps) {
  if (!hasAnyWritePermission(mcpList)) {
    return null;
  }

  return (
    <>
      {hasAnyOwnerPermission(mcpList) && bulkManageButton}
      {children}
    </>
  );
}

export function McpSelectionBarGuard({
  mcpList,
  children,
}: McpSelectionBarGuardProps) {
  if (!hasAnyOwnerPermission(mcpList)) {
    return null;
  }

  return <>{children}</>;
}

export function McpEmptyStateGuard({
  mcpList,
  addButton,
  onClickAdd,
  children,
}: McpEmptyStateGuardProps) {
  if (!hasAnyWritePermission(mcpList)) {
    return (
      <div className="flex items-center justify-between border border-dashed border-border-button rounded-md p-10 w-[590px]">
        {children}
      </div>
    );
  }

  return (
    <div
      className="flex items-center justify-between border border-dashed border-border-button rounded-md p-10 cursor-pointer w-[590px]"
      onClick={onClickAdd}
    >
      {children}
      {addButton}
    </div>
  );
}
