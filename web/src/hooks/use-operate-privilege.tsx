import { IPrivilegeManagementInitialValues } from '@/components/privilege-management/interface';
import { PermissionResourceType, TeamRole } from '@/constants/team';
import { IShareDialog } from '@/interfaces/database/team';
import { IUpdatePermission } from '@/interfaces/request/team';
import { getLlmFactoryFromLlmId } from '@/utils/private-util';
import { isEmpty } from 'lodash';
import { useCallback } from 'react';
import { useSetModalState } from './common-hooks';
import { useUpdateDialogPermission, useUpdatePermission } from './use-team';

function flatShareErrorMessages(data: IShareDialog) {
  if (isEmpty(data)) {
    return [];
  }
  return [
    ...data.failed.map((x) => ({ ...x, type: TeamRole.Member })),
    ...data.failed_groups.map((x) => ({ ...x, type: TeamRole.Group })),
    ...data.failed_departments.map((x) => ({
      ...x,
      type: TeamRole.Department,
    })),
  ];
}

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

  const handleOk = useCallback(
    async (params: IUpdatePermission) => {
      const commonParams = {
        ...params,
        resource_type:
          initialValues.resourceType || PermissionResourceType.KnowledgeBase,
        resource_id: initialValues.id,
        tenant_id: initialValues.tenant_id,
      };

      if (isDialogResourceType) {
        const data = await updateDialogPermission({
          ...commonParams,
          kbs: initialValues.kbs!,
          llm_factory: getLlmFactoryFromLlmId(initialValues.llm_id!)!,
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
        const ret = await updatePermission(commonParams);

        if (ret === 0) {
          hideAddCollaboratorDialog();
        }
      }
    },
    [
      hideAddCollaboratorDialog,
      initialValues.id,
      initialValues.kbs,
      initialValues.llm_id,
      initialValues.resourceType,
      initialValues.tenant_id,
      isDialogResourceType,
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
