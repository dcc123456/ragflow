import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog';
import { Button } from '@/components/ui/button';
import { TeamRole } from '@/constants/team';
import { TenantIdContext } from '@/contexts/teant-context';
import { useDeleteGroup } from '@/hooks/use-team';
import { IGroup } from '@/interfaces/database/team';
import { CellContext } from '@tanstack/react-table';
import { Layers, Trash2, UserCog, UserPen, UserPlus } from 'lucide-react';
import { memo, useCallback, useContext } from 'react';
import { useTranslation } from 'react-i18next';
import { GroupContext } from '../context';
import { PermissionManagementDialogContext } from '../permission-management-dialog';
import {
  useModifyGroupMember,
  useShowPartialDropdownItem,
  useTransferOwner,
} from '../use-operate-group';

type IProps = Pick<CellContext<IGroup, unknown>, 'row'> &
  Pick<ReturnType<typeof useModifyGroupMember>, 'showGroupMemberModal'> &
  Pick<ReturnType<typeof useTransferOwner>, 'showTransferOwnerModal'> & {
    userId: string;
  };

function GroupActionCell({
  row,
  showGroupMemberModal,
  showTransferOwnerModal,
  userId,
}: IProps) {
  const { t } = useTranslation();
  const record = row.original;
  const showGroupModal = useContext(GroupContext);
  const tenantId = useContext(TenantIdContext);
  const showPermissionModal = useContext(PermissionManagementDialogContext);

  const {
    showInfoEditingAndMemberManagementDropdownItem,
    showOwnerTransferAndMemberDeletingDropdownItem,
  } = useShowPartialDropdownItem(userId, record.members);

  const { deleteGroup } = useDeleteGroup(tenantId);

  const handleDeleteGroup = useCallback(() => {
    deleteGroup(row.original.group_id);
  }, [deleteGroup, row.original.group_id]);

  const handleShowGroupModal = useCallback(() => {
    showGroupModal?.(row.original);
  }, [row.original, showGroupModal]);

  const handleShowGroupMemberModal = useCallback(() => {
    showGroupMemberModal(record);
  }, [record, showGroupMemberModal]);

  const handleTransferOwnerModal = useCallback(() => {
    showTransferOwnerModal(record);
  }, [record, showTransferOwnerModal]);

  const handleShowPermissionModal = useCallback(() => {
    showPermissionModal?.({
      id: record.group_id,
      name: record.name,
      avatar: record.avatar,
      role: TeamRole.Group,
    });
  }, [showPermissionModal, record]);

  return (
    <section className="flex gap-2 items-center opacity-0 group-hover:opacity-100 transition-opacity">
      <Button
        variant="ghost"
        size="icon"
        onClick={handleShowPermissionModal}
        title={t('permission.permissionManagement')}
      >
        <Layers className="w-4 h-4" />
      </Button>
      {showInfoEditingAndMemberManagementDropdownItem && (
        <>
          <Button
            variant="ghost"
            size="icon"
            onClick={handleShowGroupModal}
            title={t('common.edit')}
          >
            <UserPen className="w-4 h-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={handleShowGroupMemberModal}
            title={t('permission.manageMember')}
          >
            <UserCog className="w-4 h-4" />
          </Button>
        </>
      )}
      {showOwnerTransferAndMemberDeletingDropdownItem && (
        <>
          <Button
            variant="ghost"
            size="icon"
            onClick={handleTransferOwnerModal}
            title={t('permission.transferOwner')}
          >
            <UserPlus className="w-4 h-4" />
          </Button>
          <ConfirmDeleteDialog onOk={handleDeleteGroup}>
            <Button variant="ghost" size="icon" title={t('common.delete')}>
              <Trash2 className="w-4 h-4" />
            </Button>
          </ConfirmDeleteDialog>
        </>
      )}
    </section>
  );
}

export const ActionCell = memo(GroupActionCell);
