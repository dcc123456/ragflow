import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useDeleteEvaluationRun } from '@/hooks/use-evaluation-request';
import { Trash2 } from 'lucide-react';
import { MouseEventHandler, PropsWithChildren, useCallback } from 'react';
import { useTranslation } from 'react-i18next';

export function EvaluationRunDropdown({
  children,
  runId,
}: PropsWithChildren<{
  runId: string;
}>) {
  const { t } = useTranslation();
  const { deleteEvaluationRun } = useDeleteEvaluationRun();

  const handleDelete: MouseEventHandler<HTMLDivElement> =
    useCallback(async () => {
      await deleteEvaluationRun(runId);
    }, [deleteEvaluationRun, runId]);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent>
        <ConfirmDeleteDialog onOk={handleDelete}>
          <DropdownMenuItem
            className="text-state-error"
            onSelect={(e) => {
              e.preventDefault();
            }}
            onClick={(e) => {
              e.stopPropagation();
            }}
          >
            {t('common.delete')} <Trash2 />
          </DropdownMenuItem>
        </ConfirmDeleteDialog>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
