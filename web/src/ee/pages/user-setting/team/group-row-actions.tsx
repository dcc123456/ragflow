import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import { Button } from '@/components/ui/button';
import { GroupPermission, hasResourcePermission } from '@/ee/constants/team';
import { ITenantGroup } from '@/ee/interfaces/database/team';
import {
  ArrowLeftRight,
  LayersPlus,
  Pencil,
  Trash2,
  UserCog,
} from 'lucide-react';
import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { GroupFormDialog } from './group-form-dialog';
import { GroupMembersDialog } from './group-members-dialog';
import { TransferOwnerDialog } from './transfer-owner-dialog';
import { useDeleteTenantGroup } from './use-group-query';

type ActiveGroupDialog = 'edit' | 'subgroup' | 'members' | 'transfer' | null;

export function GroupRowActions({
  group,
  parentPermission,
}: {
  group: ITenantGroup;
  parentPermission: number;
}) {
  const { t } = useTranslation();
  const [activeDialog, setActiveDialog] = useState<ActiveGroupDialog>(null);
  const { deleteGroup } = useDeleteTenantGroup(group.id);

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
  const canDelete =
    hasResourcePermission(permission, GroupPermission.Owner) ||
    hasResourcePermission(parentPermission, GroupPermission.DeleteSubgroup);

  const handleShowEdit = useCallback(() => {
    setActiveDialog('edit');
  }, []);

  const handleShowSubgroup = useCallback(() => {
    setActiveDialog('subgroup');
  }, []);

  const handleShowMembers = useCallback(() => {
    setActiveDialog('members');
  }, []);

  const handleShowTransferOwner = useCallback(() => {
    setActiveDialog('transfer');
  }, []);

  const handleHideDialog = useCallback(() => {
    setActiveDialog(null);
  }, []);

  const handleDelete = useCallback(async () => {
    await deleteGroup();
  }, [deleteGroup]);

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
      {canManageMembers && (
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
      {canTransferOwner && (
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={handleShowTransferOwner}
          aria-label={t('permission.transferOwner')}
          title={t('permission.transferOwner')}
        >
          <ArrowLeftRight />
        </Button>
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
      {activeDialog === 'transfer' && (
        <TransferOwnerDialog group={group} hideModal={handleHideDialog} />
      )}
    </div>
  );
}
