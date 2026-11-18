import { Button } from '@/components/ui/button';
import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  Permission,
  PermissionResourceType,
  TenantRole,
  hasResourcePermission,
} from '@/ee/constants/team';
import { useRowSelection } from '@/hooks/logic-hooks/use-row-selection';
import { useAddCollaboratorDialog } from '@/ee/hooks/use-operate-privilege';
import {
  useFetchPermissionList,
  useTransferResourceOwnership,
} from '@/ee/hooks/use-team';
import {
  useFetchUserInfo,
  useListTenantUser,
} from '@/hooks/use-user-setting-request';
import { IModalProps } from '@/interfaces/common';
import { TenantMembershipStatus } from '@/pages/user-setting/constants';
import { KeyRound, UserPlus } from 'lucide-react';
import { useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { BulkOperateBar } from '@/components/bulk-operate-bar';
import { LlmIcon } from '@/components/svg-icon';
import { AddCollaboratorDialog } from '@/ee/components/privilege-management/add-collaborator-dialog';
import { IPrivilegeManagementInitialValues } from '@/ee/components/privilege-management/interface';
import { IPermission } from '@/ee/interfaces/database/team';
import { ManagePrivilegeTable } from '@/ee/components/privilege-management/manage-privilege-dialog/manage-privilege-table';
import { useBulkOperatePrivilege } from '@/ee/components/privilege-management/manage-privilege-dialog/use-bulk-operate-priviledge';

export function PrivilegeManagementDialog({
  hideModal,
  initialValues,
}: IModalProps<any> & { initialValues: IPrivilegeManagementInitialValues }) {
  const { t } = useTranslation();
  const resourceType =
    initialValues.resourceType || PermissionResourceType.KnowledgeBase;
  const {
    showAddCollaboratorDialog,
    hideAddCollaboratorDialog,
    addCollaboratorDialogVisible,
    onOk,
    loading,
  } = useAddCollaboratorDialog(initialValues);

  const isDocumentResourceType =
    resourceType === PermissionResourceType.Document;

  const resourceIds =
    resourceType === PermissionResourceType.Document
      ? initialValues.documents || []
      : [initialValues.id];
  const resourceId = resourceIds[0];

  const { data } = useFetchPermissionList(
    initialValues.tenant_id,
    resourceIds,
    resourceType,
  );
  const { data: userInfo } = useFetchUserInfo();
  const { data: tenantMembers } = useListTenantUser(initialValues.tenant_id);
  const { transferResourceOwner, loading: transferOwnerLoading } =
    useTransferResourceOwnership();
  const currentTenantMember = tenantMembers.find(
    (member) => member.user_id === userInfo.id,
  );
  const currentMemberPermission = data.find(
    (permission) => permission.id === currentTenantMember?.id,
  );
  const isActiveTenantMember =
    currentTenantMember?.status === TenantMembershipStatus.Joined;
  const currentMemberPermissions = currentMemberPermission?.permissions ?? {};
  const hasOwnerPermissionForAllResources =
    resourceIds.length > 0 &&
    (resourceIds.length === 1
      ? hasResourcePermission(
          initialValues.operator_permission ?? 0,
          Permission.Owner,
        ) ||
        hasResourcePermission(
          currentMemberPermissions[resourceIds[0]] ?? 0,
          Permission.Owner,
        )
      : resourceIds.every((id) =>
          hasResourcePermission(
            currentMemberPermissions[id] ?? 0,
            Permission.Owner,
          ),
        ));
  const canGrantManage =
    userInfo.is_superuser ||
    (isActiveTenantMember &&
      (currentTenantMember?.role === TenantRole.Owner ||
        hasOwnerPermissionForAllResources));
  const canTransferOwner = resourceIds.length === 1 && canGrantManage;

  const tenantMemberUserIds = useMemo(
    () =>
      new Map(
        tenantMembers
          .filter((member) => member.status === TenantMembershipStatus.Joined)
          .map((member) => [member.id, member.user_id]),
      ),
    [tenantMembers],
  );
  const handleTransferOwner = useCallback(
    async (record: IPermission) => {
      const userId = tenantMemberUserIds.get(record.id);
      if (!resourceId || !userId) return;
      await transferResourceOwner({
        tenant_id: initialValues.tenant_id,
        resource_type: resourceType,
        resource_id: resourceId,
        user_id: userId,
      });
    },
    [
      initialValues.tenant_id,
      resourceId,
      resourceType,
      tenantMemberUserIds,
      transferResourceOwner,
    ],
  );

  const { rowSelection, rowSelectionIsEmpty, setRowSelection, selectedCount } =
    useRowSelection();

  const { list } = useBulkOperatePrivilege({
    rowSelection,
    setRowSelection,
    initialValues,
    permissions: data,
    canGrantManage,
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
              <span>{initialValues.documents?.length ?? 0} files</span>
            ) : (
              <div className="flex items-center gap-4">
                {initialValues.resourceType === PermissionResourceType.LLM ? (
                  <LlmIcon name={initialValues.name} />
                ) : (
                  <RAGFlowAvatar
                    name={initialValues.name}
                    avatar={initialValues.avatar || initialValues.icon}
                    className="size-6"
                  />
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

          <div className="flex items-center gap-2">
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
          canGrantManage={canGrantManage}
          canTransferOwner={canTransferOwner}
          transferOwnerLoading={transferOwnerLoading}
          tenantMemberUserIds={tenantMemberUserIds}
          onTransferOwner={handleTransferOwner}
        ></ManagePrivilegeTable>
      </DialogContent>
      {addCollaboratorDialogVisible && (
        <AddCollaboratorDialog
          hideModal={hideAddCollaboratorDialog}
          onOk={onOk}
          loading={loading}
          tenantId={initialValues.tenant_id}
          canGrantManage={canGrantManage}
          resourceType={resourceType}
        ></AddCollaboratorDialog>
      )}
    </Dialog>
  );
}
