import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import { Button } from '@/components/ui/button';
import { TeamRole } from '@/constants/team';
import { useDeleteTenantUser } from '@/hooks/use-user-setting-request';
import { ITenantUser } from '@/interfaces/database/user-setting';
import { CellContext } from '@tanstack/react-table';
import { Trash2 } from 'lucide-react';
import { useCallback, useContext } from 'react';
import { useTranslation } from 'react-i18next';
import { PermissionManagementDialogContext } from '../../permission-management-dialog';
import { useIsMyCreatedTeam } from '../../use-operate-team';

type IProps = Pick<CellContext<ITenantUser, unknown>, 'row'>;

export function ActionCell({ row }: IProps) {
  const record = row.original;
  const isMyCreatedTeam = useIsMyCreatedTeam();
  const showPermissionModal = useContext(PermissionManagementDialogContext);
  const { t } = useTranslation();

  const { deleteTenantUser } = useDeleteTenantUser();

  const handleOk = useCallback(() => {
    deleteTenantUser({ userId: record.user_id });
  }, [deleteTenantUser, record.user_id]);

  const handleShowPermissionModal = useCallback(() => {
    showPermissionModal?.({
      id: record.user_id,
      name: record.nickname,
      avatar: record.avatar,
      email: record.email,
      role: TeamRole.Member,
    });
  }, [showPermissionModal, record]);

  return (
    isMyCreatedTeam && (
      <section className="flex gap-2 items-center opacity-0 group-hover:opacity-100 transition-opacity">
        {/* <Button
          variant="ghost"
          size="icon"
          onClick={handleShowPermissionModal}
          title="Manage permissions"
        >
          <Layers className="w-4 h-4" />
        </Button> */}
        <ConfirmDeleteDialog
          onOk={handleOk}
          title={t('deleteModal.delMember')}
          content={{
            node: (
              <ConfirmDeleteDialogNode
                avatar={{ avatar: record.avatar, name: record.nickname }}
                name={record.nickname}
              />
            ),
          }}
        >
          <Button variant="ghost" size="icon" title="Delete">
            <Trash2 className="w-4 h-4" />
          </Button>
        </ConfirmDeleteDialog>
      </section>
    )
  );
}
