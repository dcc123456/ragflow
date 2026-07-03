import { PermissionResourceType } from '@/constants/team';
import { useSetModalState } from '@/hooks/common-hooks';
import { IMemory } from '@/pages/memories/interface';
import { useCallback, useMemo, useState } from 'react';

export function useMemoryWithSourceType(record: IMemory) {
  return useMemo(() => {
    return {
      ...record,
      resourceType: PermissionResourceType.Memory,
    };
  }, [record]);
}

export function useShowPrivilegeDialog() {
  const {
    visible: privilegeModalVisible,
    hideModal: hidePrivilegeModal,
    showModal: showPrivilegeModal,
  } = useSetModalState();

  const [record, setRecord] = useState<IMemory>({} as IMemory);

  const handShowPrivilegeModal = useCallback(
    (item: IMemory) => () => {
      setRecord(item);
      showPrivilegeModal();
    },
    [showPrivilegeModal],
  );

  const recordWithSourceType = useMemoryWithSourceType(record);

  return {
    privilegeModalVisible,
    hidePrivilegeModal,
    handShowPrivilegeModal,
    recordWithSourceType,
  };
}
