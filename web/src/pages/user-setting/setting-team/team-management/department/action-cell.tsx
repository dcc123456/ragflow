import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import { Button } from '@/components/ui/button';
import { TeamRole } from '@/constants/team';
import {
  useDeleteDepartment,
  useDeleteDepartmentMember,
} from '@/hooks/use-team';
import { IDepartment, IMember } from '@/interfaces/database/team';
import { CellContext } from '@tanstack/react-table';
import { ArrowLeftRight, Layers, Trash2, UserPen } from 'lucide-react';
import { useCallback, useContext } from 'react';
import { useTranslation } from 'react-i18next';
import { PermissionManagementDialogContext } from '../permission-management-dialog';
import {
  useModifyDepartment,
  useShowMoveDepartmentDialog,
} from '../use-operate-department';
import { useIsMyCreatedTeam, useTenantId } from '../use-operate-team';

type IProps = Pick<CellContext<IDepartment | IMember, unknown>, 'row'> &
  Pick<ReturnType<typeof useModifyDepartment>, 'showDepartmentModal'> &
  Pick<
    ReturnType<typeof useShowMoveDepartmentDialog>,
    'showMoveDepartmentModal'
  > & { parentDepartmentId?: string };

export function ActionCell({
  row,
  showDepartmentModal,
  showMoveDepartmentModal,
  parentDepartmentId,
}: IProps) {
  const { t } = useTranslation();
  const { deleteDepartment } = useDeleteDepartment();
  const tenantId = useTenantId();
  const isMyCreatedTeam = useIsMyCreatedTeam();
  const showPermissionModal = useContext(PermissionManagementDialogContext);
  const { deleteDepartmentMember } = useDeleteDepartmentMember(tenantId);

  const handleDeleteDepartment = useCallback(() => {
    const record = row.original;
    if ('department_id' in record) {
      deleteDepartment(record.department_id);
    } else {
      if (parentDepartmentId) {
        deleteDepartmentMember({
          department_id: parentDepartmentId,
          member_list: [{ member_id: record.member_id, role: 'member' }],
        });
      }
    }
  }, [
    deleteDepartment,
    deleteDepartmentMember,
    parentDepartmentId,
    row.original,
  ]);

  const handleShowFileRenameModal = useCallback(() => {
    showDepartmentModal(row.original);
  }, [row.original, showDepartmentModal]);

  const handleShowMoveDepartmentModal = useCallback(() => {
    const record = row.original;
    if ('department_id' in record) {
      showMoveDepartmentModal(record.department_id);
    }
  }, [row.original, showMoveDepartmentModal]);

  const handleShowPermissionModal = useCallback(() => {
    const record = row.original;
    if ('department_id' in record) {
      showPermissionModal?.({
        id: record.department_id,
        name: record.name,
        avatar: record.avatar,
        role: TeamRole.Department,
      });
    }
  }, [showPermissionModal, row.original]);

  const isDepartment = 'department_id' in row.original;
  const displayName = isDepartment
    ? (row.original as IDepartment).name
    : (row.original as IMember).nickname;
  const avatar = row.original.avatar;

  if (!isMyCreatedTeam) {
    return null;
  }

  return (
    isMyCreatedTeam && (
      <section className="flex gap-2 items-center opacity-0 group-hover:opacity-100 transition-opacity">
        {isDepartment && (
          <>
            <Button
              variant="ghost"
              size="icon"
              onClick={handleShowPermissionModal}
              title={t('permission.permissionManagement')}
            >
              <Layers className="w-4 h-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={handleShowFileRenameModal}
              title={t('common.edit')}
            >
              <UserPen className="w-4 h-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={handleShowMoveDepartmentModal}
              title={t('common.move')}
            >
              <ArrowLeftRight className="w-4 h-4" />
            </Button>
          </>
        )}
        <ConfirmDeleteDialog
          onOk={handleDeleteDepartment}
          title={t('deleteModal.delDepartment')}
          content={{
            node: (
              <ConfirmDeleteDialogNode
                avatar={{
                  avatar: avatar,
                  name: displayName,
                }}
                name={displayName}
              />
            ),
          }}
        >
          <Button variant="ghost" size="icon" title={t('common.delete')}>
            <Trash2 className="w-4 h-4" />
          </Button>
        </ConfirmDeleteDialog>
      </section>
    )
  );
}
