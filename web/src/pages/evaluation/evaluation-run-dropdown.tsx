import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useDeleteEvaluationRun } from '@/hooks/use-evaluation-request';
import { IEvaluationRun } from '@/interfaces/database/evaluation';
import { PenLine, Trash2 } from 'lucide-react';
import { MouseEventHandler, PropsWithChildren, useCallback } from 'react';
import { useTranslation } from 'react-i18next';

export function EvaluationRunDropdown({
  children,
  run,
  onRename,
}: PropsWithChildren<{
  run: IEvaluationRun;
  onRename: (run: IEvaluationRun) => void;
}>) {
  const { t } = useTranslation();
  const { deleteEvaluationRun } = useDeleteEvaluationRun();

  const handleDelete: MouseEventHandler<HTMLDivElement> =
    useCallback(async () => {
      await deleteEvaluationRun(run.id);
    }, [deleteEvaluationRun, run.id]);

  const handleRename: MouseEventHandler<HTMLDivElement> = useCallback(
    (e) => {
      e.stopPropagation();
      onRename(run);
    },
    [onRename, run],
  );

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem onClick={handleRename}>
          {t('common.rename')} <PenLine />
        </DropdownMenuItem>
        <DropdownMenuSeparator />
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
