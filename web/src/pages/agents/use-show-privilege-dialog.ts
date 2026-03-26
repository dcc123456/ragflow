import { PermissionResourceType } from '@/constants/team';
import { useSetModalState } from '@/hooks/common-hooks';
import { IFlow } from '@/interfaces/database/agent';
import { useCallback, useMemo, useState } from 'react';

export function useAgentWithSourceType(record: IFlow) {
  return useMemo(() => {
    return {
      ...record,
      resourceType: PermissionResourceType.Canvas,
      name: record.title,
    };
  }, [record]);
}

export function useShowPrivilegeDialog() {
  const {
    visible: privilegeModalVisible,
    hideModal: hidePrivilegeModal,
    showModal: showPrivilegeModal,
  } = useSetModalState();

  const [record, setRecord] = useState<IFlow>({} as IFlow);

  const handShowPrivilegeModal = useCallback(
    (item: IFlow) => () => {
      setRecord(item);
      showPrivilegeModal();
    },
    [showPrivilegeModal],
  );

  const recordWithSourceType = useAgentWithSourceType(record);

  return {
    privilegeModalVisible,
    hidePrivilegeModal,
    handShowPrivilegeModal,
    recordWithSourceType,
  };
}
