import { PermissionResourceType } from '@/constants/team';
import { useSetModalState } from '@/hooks/common-hooks';
import { useSetSelectedRecord } from '@/hooks/logic-hooks';
import { IDialog } from '@/interfaces/database/chat';
import { pick } from 'lodash';
import { useMemo } from 'react';

export function useShowPrivilegeDialog() {
  const { currentRecord, setRecord } = useSetSelectedRecord<IDialog>();

  const privilegeRecord = useMemo(() => {
    const kbs =
      currentRecord.dataset_ids ??
      (currentRecord as IDialog & { kb_ids?: string[] }).kb_ids ??
      [];

    return {
      ...pick(currentRecord, ['id', 'tenant_id', 'name']),
      resourceType: PermissionResourceType.Dialog,
      icon: currentRecord.icon,
      kbs,
      llm_id: currentRecord.llm_id,
    };
  }, [currentRecord]);

  const {
    visible: privilegeModalVisible,
    hideModal: hidePrivilegeModal,
    showModal: showPrivilegeModal,
  } = useSetModalState();

  const handleShowPrivilegeModal =
    (dialog: IDialog): any =>
    (info: any) => {
      info?.domEvent?.preventDefault();
      info?.domEvent?.stopPropagation();
      setRecord(dialog);
      showPrivilegeModal();
    };

  return {
    privilegeModalVisible,
    hidePrivilegeModal,
    privilegeRecord,
    handleShowPrivilegeModal,
  };
}
