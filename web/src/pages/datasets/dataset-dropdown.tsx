import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import { PrivilegeDropdown } from '@/components/privilege/privilege-dropdown';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useDeleteKnowledge } from '@/hooks/use-knowledge-request';
import { IKnowledge } from '@/interfaces/database/knowledge';
import {
  hasManagePermissionPermission,
  hasOwnerPermission,
  showEditButton,
} from '@/utils/permission-util';
import { LucideCopy, PenLine, Trash2 } from 'lucide-react';
import { MouseEventHandler, PropsWithChildren, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import useDuplicateDataset from './use-duplicate-dataset';
import { useRenameDataset } from './use-rename-dataset';

type IDatasetDropdownProps = {
  dataset: IKnowledge;
  showPrivilegeModal(): void;
} & Pick<ReturnType<typeof useRenameDataset>, 'showDatasetRenameModal'> & {
    showDatasetDuplicateModal: ReturnType<
      typeof useDuplicateDataset
    >['showModal'];
  };

export function DatasetDropdown({
  children,
  showDatasetRenameModal,
  showDatasetDuplicateModal,
  dataset,
  showPrivilegeModal,
}: PropsWithChildren<IDatasetDropdownProps>) {
  const { t } = useTranslation();
  const { deleteKnowledge } = useDeleteKnowledge();

  const handleShowDatasetRenameModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showDatasetRenameModal(dataset);
      },
      [dataset, showDatasetRenameModal],
    );

  const handleDelete: MouseEventHandler<HTMLDivElement> = useCallback(() => {
    deleteKnowledge(dataset.id);
  }, [dataset.id, deleteKnowledge]);

  const handlesShowPrivilegeModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showPrivilegeModal();
      },
      [showPrivilegeModal],
    );

  if (!showEditButton(dataset.operator_permission)) {
    return null;
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem onClick={handleShowDatasetRenameModal}>
          {t('common.rename')} <PenLine />
        </DropdownMenuItem>

        {hasManagePermissionPermission(dataset.operator_permission) && (
          <DropdownMenuItem
            onClick={(e) => {
              e.stopPropagation();
              showDatasetDuplicateModal(dataset);
            }}
          >
            {t('common.duplicate')} <LucideCopy />
          </DropdownMenuItem>
        )}

        <DropdownMenuSeparator />

        {hasManagePermissionPermission(dataset.operator_permission) && (
          <>
            <DropdownMenuItem onClick={handlesShowPrivilegeModal}>
              <PrivilegeDropdown></PrivilegeDropdown>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
          </>
        )}

        {hasOwnerPermission(dataset.operator_permission) && (
          <>
            <ConfirmDeleteDialog
              onOk={handleDelete}
              content={{
                node: (
                  <ConfirmDeleteDialogNode
                    avatar={{ avatar: dataset.avatar, name: dataset.name }}
                    name={dataset.name}
                  />
                ),
              }}
              title={t('deleteModal.delDataset')}
            >
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
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
