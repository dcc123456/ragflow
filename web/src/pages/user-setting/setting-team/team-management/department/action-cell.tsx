import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  useDeleteDepartment,
  useDeleteDepartmentMember,
} from '@/hooks/use-team';
import { IDepartment, IMember } from '@/interfaces/database/team';
import { CellContext } from '@tanstack/react-table';
import { EllipsisVertical, Move, SquarePen, Trash2 } from 'lucide-react';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
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

  const isDepartment = 'department_id' in row.original;

  if (!isMyCreatedTeam) {
    return null;
  }

  return (
    <section className="flex gap-4 items-center">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="secondary" size={'icon'}>
            <EllipsisVertical />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {isDepartment && (
            <>
              <DropdownMenuItem onClick={handleShowFileRenameModal}>
                <SquarePen /> {t('common.edit')}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={handleShowMoveDepartmentModal}>
                <Move /> {t('common.move')}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
            </>
          )}
          <ConfirmDeleteDialog onOk={handleDeleteDepartment}>
            <DropdownMenuItem onSelect={(e) => e.preventDefault()}>
              <Trash2 /> {t('common.delete')}
            </DropdownMenuItem>
          </ConfirmDeleteDialog>
        </DropdownMenuContent>
      </DropdownMenu>
    </section>
  );
}
