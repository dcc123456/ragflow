import { IPrivilegeManagementInitialValues } from '@/components/privilege-management/interface';
import { PermissionResourceType } from '@/constants/team';
import { IUpdatePermission } from '@/interfaces/request/team';
import { useCallback } from 'react';
import { useSetModalState } from './common-hooks';
import { useUpdatePermission } from './use-team';

export function useAddCollaboratorDialog(
  initialValues: IPrivilegeManagementInitialValues,
) {
  const {
    visible: addCollaboratorDialogVisible,
    hideModal: hideAddCollaboratorDialog,
    showModal: showAddCollaboratorDialog,
  } = useSetModalState();
  const { updatePermission, loading } = useUpdatePermission();
  const isDocumentResourceType =
    initialValues.resourceType === PermissionResourceType.Document;

  const handleOk = useCallback(
    async (params: IUpdatePermission) => {
      const commonParams = {
        ...params,
        resource_type:
          initialValues.resourceType || PermissionResourceType.KnowledgeBase,
        tenant_id: initialValues.tenant_id,
      };

      const ret = await updatePermission({
        ...commonParams,
        resource_ids: isDocumentResourceType
          ? initialValues.documents || []
          : [initialValues.id],
      });

      if (ret === 0) {
        hideAddCollaboratorDialog();
      }
    },
    [
      hideAddCollaboratorDialog,
      initialValues.documents,
      initialValues.id,
      initialValues.resourceType,
      initialValues.tenant_id,
      isDocumentResourceType,
      updatePermission,
    ],
  );

  return {
    addCollaboratorDialogVisible,
    hideAddCollaboratorDialog,
    showAddCollaboratorDialog,
    onOk: handleOk,
    loading,
  };
}
