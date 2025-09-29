import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { PermissionResourceType } from '@/constants/team';
import { useSetModalState } from '@/hooks/common-hooks';
import { useAddCollaboratorDialog } from '@/hooks/use-operate-privilege';
import { useFetchPermissionList } from '@/hooks/use-team';
import { IModalProps } from '@/interfaces/common';
import { IPermission } from '@/interfaces/database/team';
import { KeyRound, Settings, UserPlus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { LlmIcon } from '../svg-icon';
import { AddCollaboratorDialog } from './add-collaborator-dialog';
import { IPrivilegeManagementInitialValues } from './interface';
import { ManagePrivilegeDialog } from './manage-privilege-dialog';
import { PrivilegeAvatar } from './privilege-avatar';
import { TransferOwnerDialog } from './transfer-owner-dialog';

function Item({ item }: { item: IPermission }) {
  return (
    <div className="flex items-center gap-2 rounded-md bg-colors-background-neutral-standard px-2 py-1">
      <PrivilegeAvatar avatar={item.avatar}></PrivilegeAvatar>
      <span>{item.name}</span>
    </div>
  );
}

export function PrivilegeManagementDialog({
  hideModal,
  initialValues,
}: IModalProps<any> & { initialValues: IPrivilegeManagementInitialValues }) {
  const { t } = useTranslation();
  const {
    visible: transferOwnerModalVisible,
    hideModal: hideTransferOwnerModal,
    // showModal: showTransferOwnerModal,
  } = useSetModalState();
  const {
    showAddCollaboratorDialog,
    hideAddCollaboratorDialog,
    addCollaboratorDialogVisible,
    onOk,
    loading,
  } = useAddCollaboratorDialog(initialValues);

  const {
    visible: managePrivilegeModalVisible,
    hideModal: hideManagePrivilegeModal,
    showModal: showManagePrivilegeModal,
  } = useSetModalState();

  const { data } = useFetchPermissionList(
    initialValues.tenant_id,
    initialValues.id,
    initialValues.resourceType,
  );

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex gap-4 items-center">
            <KeyRound className="size-5" />
            {t('permission.permissionManagement')}
          </DialogTitle>
        </DialogHeader>
        <div className="flex items-center gap-4">
          {initialValues.resourceType === PermissionResourceType.LLM ? (
            <LlmIcon name={initialValues.name} />
          ) : (
            <PrivilegeAvatar
              avatar={initialValues.avatar || initialValues.icon}
              className="size-10"
            ></PrivilegeAvatar>
          )}

          <span className="font-semibold text-lg">{initialValues.name}</span>
        </div>
        <section className="flex justify-between items-center">
          <span>{t('permission.collaborator')}</span>
          <div className="space-x-4">
            <Button variant={'outline'} onClick={showManagePrivilegeModal}>
              <Settings /> {t('permission.manage')}
            </Button>
            <Button variant={'outline'} onClick={showAddCollaboratorDialog}>
              <UserPlus /> {t('common.add')}
            </Button>
          </div>
        </section>
        <div className="bg-colors-background-neutral-strong p-2 flex gap-4 flex-wrap rounded-md max-h-[70vh] overflow-auto">
          {data.map((x) => (
            <Item key={x.id} item={x}></Item>
          ))}
        </div>
        {/* <div>
          <Button
            variant={'outline'}
            className="w-full"
            onClick={showTransferOwnerModal}
          >
            <ArrowLeftRight />
            Transferring Ownership
          </Button>
        </div> */}
      </DialogContent>
      {transferOwnerModalVisible && (
        <TransferOwnerDialog
          hideModal={hideTransferOwnerModal}
        ></TransferOwnerDialog>
      )}
      {addCollaboratorDialogVisible && (
        <AddCollaboratorDialog
          hideModal={hideAddCollaboratorDialog}
          onOk={onOk}
          loading={loading}
          resourceType={initialValues.resourceType}
          tenantId={initialValues.tenant_id}
        ></AddCollaboratorDialog>
      )}
      {managePrivilegeModalVisible && (
        <ManagePrivilegeDialog
          hideModal={hideManagePrivilegeModal}
          initialValues={initialValues}
        ></ManagePrivilegeDialog>
      )}
    </Dialog>
  );
}
