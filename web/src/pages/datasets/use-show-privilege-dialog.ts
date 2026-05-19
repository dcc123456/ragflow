import { useSetModalState } from '@/hooks/common-hooks';
import { useKnowledgeWithSourceType } from '@/hooks/logic-hooks/use-knowledge';
import { IDataset } from '@/interfaces/database/dataset';
import { useCallback, useState } from 'react';

export function useShowPrivilegeDialog() {
  const {
    visible: privilegeModal,
    hideModal: hidePrivilegeModal,
    showModal: showPrivilegeModal,
  } = useSetModalState();

  const [record, setRecord] = useState<IDataset>({} as IDataset);

  const handShowPrivilegeModal = useCallback(
    (item: IDataset) => () => {
      setRecord(item);
      showPrivilegeModal();
    },
    [showPrivilegeModal],
  );

  const recordWithSourceType = useKnowledgeWithSourceType(record);

  return {
    privilegeModal,
    hidePrivilegeModal,
    handShowPrivilegeModal,
    recordWithSourceType,
  };
}
