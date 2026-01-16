import { IPrivilegeManagementInitialValues } from '@/components/privilege-management/interface';
import { PermissionResourceType } from '@/constants/team';
import { IUpdatePermission } from '@/interfaces/request/team';
import { getLlmFactoryFromLlmId } from '@/utils/private-util';
import { useCallback } from 'react';
import { useSetModalState } from './common-hooks';
import { useUpdateDialogPermission, useUpdatePermission } from './use-team';

export function useAddCollaboratorDialog(
  initialValues: IPrivilegeManagementInitialValues,
) {
  const {
    visible: addCollaboratorDialogVisible,
    hideModal: hideAddCollaboratorDialog,
    showModal: showAddCollaboratorDialog,
  } = useSetModalState();
  const { updatePermission, loading } = useUpdatePermission();
  const { updateDialogPermission, loading: updateDialogPermissionLoading } =
    useUpdateDialogPermission();

  const isDialogResourceType =
    initialValues.resourceType === PermissionResourceType.Dialog;

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

      if (isDialogResourceType) {
        const data = await updateDialogPermission({
          ...commonParams,
          kbs: initialValues.kbs!,
          llm_factory: getLlmFactoryFromLlmId(initialValues.llm_id!)!,
          resource_id: initialValues.id,
        });

        // const errorMessages = flatShareErrorMessages(data.data);

        // if (!isEmpty(errorMessages)) {
        //   toast.error(
        //     <DialogPrivilegeErrorMessage
        //       data={errorMessages}
        //     ></DialogPrivilegeErrorMessage>,
        //     { duration: 8000 },
        //   );
        // }

        if (data.code === 0) {
          hideAddCollaboratorDialog();
        }
      } else {
        const ret = await updatePermission({
          ...commonParams,
          resource_ids: isDocumentResourceType
            ? initialValues.documents || []
            : [initialValues.id],
        });

        if (ret === 0) {
          hideAddCollaboratorDialog();
        }
      }
    },
    [
      hideAddCollaboratorDialog,
      initialValues.documents,
      initialValues.id,
      initialValues.kbs,
      initialValues.llm_id,
      initialValues.resourceType,
      initialValues.tenant_id,
      isDialogResourceType,
      isDocumentResourceType,
      updateDialogPermission,
      updatePermission,
    ],
  );

  return {
    addCollaboratorDialogVisible,
    hideAddCollaboratorDialog,
    showAddCollaboratorDialog,
    onOk: handleOk,
    loading: isDialogResourceType ? updateDialogPermissionLoading : loading,
  };
}
