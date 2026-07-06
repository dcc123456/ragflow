import { PermissionResourceType } from '@/constants/team';
import { useSetModalState } from '@/hooks/common-hooks';
import { useCallback, useMemo, useState } from 'react';
import { ISearchAppProps } from './hooks';

export function useSearchWithSourceType(record: ISearchAppProps) {
  return useMemo(() => {
    return {
      ...record,
      resourceType: PermissionResourceType.Search,
    };
  }, [record]);
}

export function useShowPrivilegeDialog() {
  const {
    visible: privilegeModalVisible,
    hideModal: hidePrivilegeModal,
    showModal: showPrivilegeModal,
  } = useSetModalState();

  const [record, setRecord] = useState<ISearchAppProps>({} as ISearchAppProps);

  const handShowPrivilegeModal = useCallback(
    (item: ISearchAppProps) => () => {
      setRecord(item);
      showPrivilegeModal();
    },
    [showPrivilegeModal],
  );

  const recordWithSourceType = useSearchWithSourceType(record);

  return {
    privilegeModalVisible,
    hidePrivilegeModal,
    handShowPrivilegeModal,
    recordWithSourceType,
  };
}
