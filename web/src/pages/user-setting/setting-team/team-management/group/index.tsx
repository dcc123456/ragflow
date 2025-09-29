import { TransferOwnerDialog } from '@/components/privilege-management/transfer-owner-dialog';
import { TriggerMemberManagementDialogType } from '../constant';
import { MemberManagementDialog } from '../member-management-dialog';
import { useModifyGroupMember, useTransferOwner } from '../use-operate-group';
import { GroupTable } from './group-table';

export function Group() {
  const {
    showGroupMemberModal,
    hideGroupMemberModal,
    onGroupMemberOk,
    groupMemberVisible,
    groupMember,
  } = useModifyGroupMember();
  const {
    transferOwnerVisible,
    hideTransferOwnerModal,
    showTransferOwnerModal,
    onTransferOwnerOk,
    groupMember: transferGroupMember,
  } = useTransferOwner();

  return (
    <section>
      <GroupTable
        showGroupMemberModal={showGroupMemberModal}
        showTransferOwnerModal={showTransferOwnerModal}
      ></GroupTable>
      {groupMemberVisible && (
        <MemberManagementDialog
          hideModal={hideGroupMemberModal}
          initialId={groupMember.group_id}
          onOk={onGroupMemberOk}
          triggerMemberManagementDialogType={
            TriggerMemberManagementDialogType.Group
          }
        ></MemberManagementDialog>
      )}
      {transferOwnerVisible && (
        <TransferOwnerDialog
          hideModal={hideTransferOwnerModal}
          onOk={onTransferOwnerOk}
          initialValues={transferGroupMember}
        ></TransferOwnerDialog>
      )}
    </section>
  );
}
