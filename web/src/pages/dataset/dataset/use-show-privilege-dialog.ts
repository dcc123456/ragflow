import { PermissionResourceType } from '@/constants/team';
import { useSetModalState } from '@/hooks/common-hooks';
import { useFetchKnowledgeBaseConfiguration } from '@/hooks/use-knowledge-request';
import { IDocumentInfo } from '@/interfaces/database/document';
import { createContext, useCallback, useState } from 'react';

export function useShowPrivilegeDialog() {
  const {
    visible: privilegeModal,
    hideModal: hidePrivilegeModal,
    showModal: showPrivilegeModal,
  } = useSetModalState();
  const { data } = useFetchKnowledgeBaseConfiguration();

  const [record, setRecord] = useState<IDocumentInfo>({} as IDocumentInfo);
  const [selectedRows, setSelectedRows] = useState<string[]>([]);

  const handShowPrivilegeModal = useCallback(
    (item: IDocumentInfo | string[]) => () => {
      if (Array.isArray(item)) {
        setSelectedRows(item);
        setRecord({} as IDocumentInfo);
      } else {
        setRecord(item);
        setSelectedRows([]);
      }
      showPrivilegeModal();
    },
    [showPrivilegeModal],
  );

  const recordWithSourceType = {
    ...record,
    resourceType: PermissionResourceType.Document,
    tenant_id: data.tenant_id,
    documents: selectedRows.length > 0 ? selectedRows : [record.id],
  };

  return {
    privilegeModal,
    hidePrivilegeModal,
    handShowPrivilegeModal,
    recordWithSourceType,
  };
}

export type ShowPrivilegeModalReturnType = ReturnType<
  typeof useShowPrivilegeDialog
>;

export const ShowPrivilegeModalContext = createContext<
  Partial<ShowPrivilegeModalReturnType>
>({});
