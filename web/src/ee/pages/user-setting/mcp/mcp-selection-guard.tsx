import { hasOwnerPermission } from '@/ee/utils/permission-util';
import { IMcpServer } from '@/interfaces/database/mcp';
import { ReactNode } from 'react';

type McpSelectionGuardProps = {
  mcp: IMcpServer;
  children: ReactNode;
};

/**
 * Collaborator-aware gating for the MCP card selection control. Only owners
 * may add a server to the bulk selection.
 */
export function McpSelectionGuard({ mcp, children }: McpSelectionGuardProps) {
  if (!hasOwnerPermission(mcp.operator_permission)) {
    return null;
  }

  return <>{children}</>;
}
