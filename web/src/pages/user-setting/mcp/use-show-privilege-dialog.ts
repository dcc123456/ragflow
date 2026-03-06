import { PermissionResourceType } from '@/constants/team';
import { useSetModalState } from '@/hooks/common-hooks';
import { IMcpServer } from '@/interfaces/database/mcp';
import { useCallback, useMemo, useState } from 'react';

export function useMcpWithSourceType(record: IMcpServer) {
  return useMemo(() => {
    return { ...record, resourceType: PermissionResourceType.MCP };
  }, [record]);
}

export function useShowPrivilegeDialog() {
  const {
    visible: privilegeModal,
    hideModal: hidePrivilegeModal,
    showModal: showPrivilegeModal,
  } = useSetModalState();

  const [record, setRecord] = useState<IMcpServer>({} as IMcpServer);

  const handShowPrivilegeModal = useCallback(
    (item: IMcpServer) => () => {
      setRecord(item);
      showPrivilegeModal();
    },
    [showPrivilegeModal],
  );

  const recordWithSourceType = useMcpWithSourceType(record);

  return {
    privilegeModal,
    hidePrivilegeModal,
    handShowPrivilegeModal,
    recordWithSourceType,
  };
}
