import { hasOwnerPermission } from '@/ee/utils/permission-util';
import { IMcpServer } from '@/interfaces/database/mcp';
import { useMemo } from 'react';

export function useOwnerMcpList(mcpList: IMcpServer[]) {
  return useMemo(
    () =>
      mcpList.filter((item) => hasOwnerPermission(item.operator_permission)),
    [mcpList],
  );
}
