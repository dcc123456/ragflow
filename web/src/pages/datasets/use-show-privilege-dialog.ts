import { useSetModalState } from '@/hooks/common-hooks';
import { useKnowledgeWithSourceType } from '@/hooks/logic-hooks/use-knowledge';
import { IKnowledge } from '@/interfaces/database/knowledge';
import { useCallback, useState } from 'react';

export function useShowPrivilegeDialog() {
  const {
    visible: privilegeModal,
    hideModal: hidePrivilegeModal,
    showModal: showPrivilegeModal,
  } = useSetModalState();

  const [record, setRecord] = useState<IKnowledge>({} as IKnowledge);

  const handShowPrivilegeModal = useCallback(
    (item: IKnowledge) => () => {
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
