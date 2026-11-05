import { TransferOwnerDialog } from '@/ee/components/privilege-management/transfer-owner-dialog';
import { TriggerMemberManagementDialogType } from '@/ee/pages/user-setting/setting-team/team-management/constant';
import { MemberManagementDialog } from '@/ee/pages/user-setting/setting-team/team-management/member-management-dialog';
import {
  useModifyGroupMember,
  useTransferOwner,
} from '@/ee/pages/user-setting/setting-team/team-management/use-operate-group';
import { GroupTable } from '@/ee/pages/user-setting/setting-team/team-management/group/group-table';

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
