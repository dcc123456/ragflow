import { hasOwnerPermission } from '@/ee/utils/permission-util';
import { IMcpServer } from '@/interfaces/database/mcp';
import { useEffect } from 'react';

export function useExitMcpSelectionWithoutOwner(
  mcpList: IMcpServer[],
  isSelectionMode: boolean,
  exitSelectionMode: () => void,
) {
  const hasOwnerMcp = mcpList?.some((item) =>
    hasOwnerPermission(item.operator_permission),
  );

  useEffect(() => {
    if (!hasOwnerMcp && isSelectionMode) {
      exitSelectionMode();
    }
  }, [hasOwnerMcp, isSelectionMode, exitSelectionMode]);
}
