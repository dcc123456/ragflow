import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { TenantIdContext } from '@/contexts/teant-context';
import { useDeleteGroup } from '@/hooks/use-team';
import { IGroup } from '@/interfaces/database/team';
import { CellContext } from '@tanstack/react-table';
import {
  ArrowRightLeft,
  EllipsisVertical,
  SquarePen,
  Trash2,
  Users,
} from 'lucide-react';
import { memo, useCallback, useContext } from 'react';
import { useTranslation } from 'react-i18next';
import { GroupContext } from '../context';
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

  return (
    <section className="flex gap-4 items-center">
      {(showInfoEditingAndMemberManagementDropdownItem ||
        showOwnerTransferAndMemberDeletingDropdownItem) && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="secondary" size={'icon'}>
              <EllipsisVertical />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {showInfoEditingAndMemberManagementDropdownItem && (
              <>
                <DropdownMenuItem onClick={handleShowGroupModal}>
                  <SquarePen /> {t('common.edit')}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={handleShowGroupMemberModal}>
                  <Users />
                  {t('permission.manageMember')}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
              </>
            )}
            {showOwnerTransferAndMemberDeletingDropdownItem && (
              <>
                <DropdownMenuItem onClick={handleTransferOwnerModal}>
                  <ArrowRightLeft /> {t('permission.transferOwner')}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <ConfirmDeleteDialog onOk={handleDeleteGroup}>
                  <DropdownMenuItem onSelect={(e) => e.preventDefault()}>
                    <Trash2 /> {t('common.delete')}
                  </DropdownMenuItem>
                </ConfirmDeleteDialog>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </section>
  );
}

export const ActionCell = memo(GroupActionCell);
