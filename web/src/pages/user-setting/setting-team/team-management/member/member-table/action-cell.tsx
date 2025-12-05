import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog';
import { Button } from '@/components/ui/button';
import { useDeleteTenantUser } from '@/hooks/use-user-setting-request';
import { ITenantUser } from '@/interfaces/database/user-setting';
import { CellContext } from '@tanstack/react-table';
import { Trash2 } from 'lucide-react';
import { useCallback } from 'react';
import { useIsMyCreatedTeam } from '../../use-operate-team';

type IProps = Pick<CellContext<ITenantUser, unknown>, 'row'>;

export function ActionCell({ row }: IProps) {
  const record = row.original;
  const isMyCreatedTeam = useIsMyCreatedTeam();

  const { deleteTenantUser } = useDeleteTenantUser();

  const handleOk = useCallback(() => {
    deleteTenantUser({ userId: record.user_id });
  }, [deleteTenantUser, record.user_id]);

  return (
    isMyCreatedTeam && (
      <ConfirmDeleteDialog onOk={handleOk}>
        <Button variant="secondary" size={'icon'}>
          <Trash2 />
        </Button>
      </ConfirmDeleteDialog>
    )
  );
}
