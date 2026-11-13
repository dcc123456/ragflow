import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import { Button } from '@/components/ui/button';
import {
  GroupMemberStatus,
  GroupPermission,
  hasResourcePermission,
} from '@/ee/constants/team';
import { ITenantGroup } from '@/ee/interfaces/database/team';
import { useFetchUserInfo } from '@/hooks/use-user-setting-request';
import {
  LayersPlus,
  LogOut,
  Move,
  Pencil,
  Trash2,
  UserCog,
} from 'lucide-react';
import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { GroupFormDialog } from './group-form-dialog';
import { GroupMembersDialog } from './group-members-dialog';
import { MoveGroupDialog } from './move-group-dialog';
import {
  useDeleteTenantGroup,
  useRemoveTenantGroupMember,
  useTenantGroupMembers,
} from './use-group-query';

type ActiveGroupDialog = 'edit' | 'subgroup' | 'members' | 'move' | null;

export function GroupRowActions({
  group,
  parentPermission,
}: {
  group: ITenantGroup;
  parentPermission: number;
}) {
  const { t } = useTranslation();
  const [activeDialog, setActiveDialog] = useState<ActiveGroupDialog>(null);
  const { data: userInfo } = useFetchUserInfo();
  const { data: members } = useTenantGroupMembers(
    group.id,
    group.member_count > 0,
  );
  const { deleteGroup } = useDeleteTenantGroup(group.id);
  const { loading: leaving, removeMember } = useRemoveTenantGroupMember(
    group.id,
  );

  const permission = group.permission;
  const canEdit = hasResourcePermission(permission, GroupPermission.Owner);
  const canCreateSubgroup = hasResourcePermission(
    permission,
    GroupPermission.AddSubgroup,
  );
  const canManageMembers =
    hasResourcePermission(permission, GroupPermission.AddMember) ||
    hasResourcePermission(permission, GroupPermission.RemoveMember) ||
    hasResourcePermission(
      permission,
      GroupPermission.ConfigureMemberPermissions,
    );
  const canTransferOwner = hasResourcePermission(
    permission,
    GroupPermission.Owner,
  );
  const canMoveGroup = group.parent_id
    ? hasResourcePermission(parentPermission, GroupPermission.DeleteSubgroup)
    : canTransferOwner;
  const canOpenMemberManagement = canManageMembers || canTransferOwner;
  const canDelete =
    hasResourcePermission(permission, GroupPermission.Owner) ||
    hasResourcePermission(parentPermission, GroupPermission.DeleteSubgroup);
  const isMember =
    group.is_member ||
    members.some(
      (member) =>
        member.user_id === userInfo.id &&
        member.status === GroupMemberStatus.Joined,
    );
  const canLeave = isMember && !group.is_owner && !!userInfo.id;

  const handleShowEdit = useCallback(() => {
    setActiveDialog('edit');
  }, []);

  const handleShowSubgroup = useCallback(() => {
    setActiveDialog('subgroup');
  }, []);

  const handleShowMembers = useCallback(() => {
    setActiveDialog('members');
  }, []);

  const handleShowMoveGroup = useCallback(() => {
    setActiveDialog('move');
  }, []);

  const handleHideDialog = useCallback(() => {
    setActiveDialog(null);
  }, []);

  const handleDelete = useCallback(async () => {
    await deleteGroup();
  }, [deleteGroup]);

  const handleLeave = useCallback(async () => {
    if (userInfo.id) {
      await removeMember(userInfo.id);
    }
  }, [removeMember, userInfo.id]);

  return (
    <div className="flex items-center justify-end gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
      {canEdit && (
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={handleShowEdit}
          aria-label={t('common.edit')}
          title={t('common.edit')}
        >
          <Pencil />
        </Button>
      )}
      {canCreateSubgroup && (
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={handleShowSubgroup}
          aria-label={t('permission.createSubgroup')}
          title={t('permission.createSubgroup')}
        >
          <LayersPlus />
        </Button>
      )}
      {canOpenMemberManagement && (
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={handleShowMembers}
          aria-label={t('permission.manageMember')}
          title={t('permission.manageMember')}
        >
          <UserCog />
        </Button>
      )}
      {canMoveGroup && (
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={handleShowMoveGroup}
          aria-label={t('common.move')}
          title={t('common.move')}
        >
          <Move />
        </Button>
      )}
      {canLeave && (
        <ConfirmDeleteDialog
          onOk={handleLeave}
          title={t('permission.leaveGroup')}
          okButtonText={t('permission.leaveGroup')}
          content={{
            node: (
              <ConfirmDeleteDialogNode
                avatar={{ avatar: group.avatar, name: group.name }}
                name={group.name}
                warnText={t('permission.leaveGroupHint')}
              />
            ),
          }}
        >
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={leaving}
            aria-label={t('permission.leaveGroup')}
            title={t('permission.leaveGroup')}
          >
            <LogOut />
          </Button>
        </ConfirmDeleteDialog>
      )}
      {canDelete && (
        <ConfirmDeleteDialog
          onOk={handleDelete}
          title={t('deleteModal.delGroup')}
          content={{
            node: (
              <ConfirmDeleteDialogNode
                avatar={{ avatar: group.avatar, name: group.name }}
                name={group.name}
              />
            ),
          }}
        >
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t('common.delete')}
            title={t('common.delete')}
          >
            <Trash2 />
          </Button>
        </ConfirmDeleteDialog>
      )}

      {activeDialog === 'edit' && (
        <GroupFormDialog group={group} hideModal={handleHideDialog} />
      )}
      {activeDialog === 'subgroup' && (
        <GroupFormDialog parentGroup={group} hideModal={handleHideDialog} />
      )}
      {activeDialog === 'members' && (
        <GroupMembersDialog group={group} hideModal={handleHideDialog} />
      )}
      {activeDialog === 'move' && (
        <MoveGroupDialog group={group} hideModal={handleHideDialog} />
      )}
    </div>
  );
}
