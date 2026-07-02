import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { PermissionResourceType } from '@/constants/team';
import { useSetModalState } from '@/hooks/common-hooks';
import { useRowSelection } from '@/hooks/logic-hooks/use-row-selection';
import { useAddCollaboratorDialog } from '@/hooks/use-operate-privilege';
import { useFetchPermissionList } from '@/hooks/use-team';
import { IModalProps } from '@/interfaces/common';
import { KeyRound, UserPlus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { BulkOperateBar } from '../bulk-operate-bar';
import { LlmIcon } from '../svg-icon';
import { AddCollaboratorDialog } from './add-collaborator-dialog';
import { IPrivilegeManagementInitialValues } from './interface';
import { ManagePrivilegeTable } from './manage-privilege-dialog/manage-privilege-table';
import { useBulkOperatePrivilege } from './manage-privilege-dialog/use-bulk-operate-priviledge';
import { PrivilegeAvatar } from './privilege-avatar';
import { TransferOwnerDialog } from './transfer-owner-dialog';

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

  const resourceType =
    initialValues.resourceType || PermissionResourceType.KnowledgeBase;

  const isDocumentResourceType =
    resourceType === PermissionResourceType.Document;

  const resourceIds =
    resourceType === PermissionResourceType.Document
      ? initialValues.documents || []
      : [initialValues.id];

  const { data } = useFetchPermissionList(
    initialValues.tenant_id,
    resourceIds,
    resourceType,
  );

  const { rowSelection, rowSelectionIsEmpty, setRowSelection, selectedCount } =
    useRowSelection();

  const { list } = useBulkOperatePrivilege({
    rowSelection,
    setRowSelection,
    initialValues,
    permissions: data,
  });

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="max-w-5xl">
        <DialogHeader>
          <DialogTitle className="flex gap-4 items-center">
            <KeyRound className="size-5" />
            {t('permission.permissionManagement')}
          </DialogTitle>
        </DialogHeader>

        <section className="flex justify-between items-center">
          <div className="flex gap-2 items-center">
            {isDocumentResourceType ? (
              <span>{initialValues.documents.length} files</span>
            ) : (
              <div className="flex items-center gap-4">
                {initialValues.resourceType === PermissionResourceType.LLM ? (
                  <LlmIcon name={initialValues.name} />
                ) : (
                  <PrivilegeAvatar
                    avatar={initialValues.avatar || initialValues.icon}
                    className="size-6"
                  ></PrivilegeAvatar>
                )}

                <span className="font-semibold text-base max-w-60 truncate">
                  {initialValues.name}
                </span>
              </div>
            )}
            <span className="text-xs text-text-secondary">
              {t('permission.collaborator')}: {data.length}
            </span>
          </div>

          <div className="space-x-4">
            <Button variant={'outline'} onClick={showAddCollaboratorDialog}>
              <UserPlus /> {t('common.add')}
            </Button>
          </div>
        </section>

        {rowSelectionIsEmpty || (
          <BulkOperateBar
            list={list}
            count={selectedCount}
            unit={t('permission.collaborator')}
          ></BulkOperateBar>
        )}
        <ManagePrivilegeTable
          initialValues={initialValues}
          rowSelection={rowSelection}
          setRowSelection={setRowSelection}
          data={data}
          resourceType={resourceType}
        ></ManagePrivilegeTable>
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
          tenantId={initialValues.tenant_id}
        ></AddCollaboratorDialog>
      )}
    </Dialog>
  );
}
