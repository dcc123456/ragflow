import { IPrivilegeManagementInitialValues } from '@/components/privilege-management/interface';
import { PrivilegeManagementDialog } from '@/components/privilege-management/privilege-management-dialog';
import { PermissionResourceType } from '@/constants/team';
import { useSetModalState } from '@/hooks/common-hooks';
import { LlmItem, useSelectLlmList } from '@/hooks/use-llm-request';
import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import { t } from 'i18next';
import { useCallback, useState } from 'react';
import { ModelProviderCard } from './modal-card';

export const UsedModel = ({
  handleAddModel,
  handleEditModel,
}: {
  handleAddModel: (factory: string) => void;
  handleEditModel: (model: any, factory: LlmItem) => void;
}) => {
  const { myLlmList: llmList } = useSelectLlmList();

  const { data: tenantInfo = {} } = useFetchTenantInfo();

  const {
    visible: privilegeModal,
    hideModal: hidePrivilegeModal,
    showModal: showPrivilegeModal,
  } = useSetModalState();

  const [record, setRecord] = useState<IPrivilegeManagementInitialValues>(
    {} as IPrivilegeManagementInitialValues,
  );
  const handShowPrivilegeModal = useCallback(
    (item: Omit<IPrivilegeManagementInitialValues, 'tenant_id'>) => {
      setRecord({
        ...item,
        tenant_id: tenantInfo.tenant_id,
        resourceType: PermissionResourceType.LLM,
      });
      showPrivilegeModal();
    },
    [showPrivilegeModal, tenantInfo.tenant_id],
  );

  return (
    <div
      className="flex flex-col w-full gap-5 mb-4"
      data-testid="added-models-section"
    >
      <div className="text-text-primary text-2xl font-medium mb-2 mt-4">
        {t('setting.addedModels')}
      </div>
      {llmList.map((llm) => {
        return (
          <ModelProviderCard
            key={llm.name}
            item={llm}
            clickApiKey={handleAddModel}
            handleEditModel={handleEditModel}
            showPrivilegeModal={handShowPrivilegeModal}
          />
        );
      })}

      {privilegeModal && (
        <PrivilegeManagementDialog
          hideModal={hidePrivilegeModal}
          initialValues={record}
        ></PrivilegeManagementDialog>
      )}
    </div>
  );
};
