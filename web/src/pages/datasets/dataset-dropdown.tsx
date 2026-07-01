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
import { IDataset } from '@/interfaces/database/dataset';
import {
  hasManagePermissionPermission,
  hasOwnerPermission,
} from '@/utils/permission-util';
import { PenLine, Trash2 } from 'lucide-react';
import { MouseEventHandler, PropsWithChildren, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import useDuplicateDataset from './use-duplicate-dataset';
import { useRenameDataset } from './use-rename-dataset';

type IDatasetDropdownProps = {
  dataset: IDataset;
  showPrivilegeModal(): void;
} & Pick<ReturnType<typeof useRenameDataset>, 'showDatasetRenameModal'> & {
    showDatasetDuplicateModal: ReturnType<
      typeof useDuplicateDataset
    >['showModal'];
  };

export function DatasetDropdown({
  children,
  showDatasetRenameModal,
  // showDatasetDuplicateModal,
  dataset,
  showPrivilegeModal,
}: PropsWithChildren<IDatasetDropdownProps> &
  Pick<ReturnType<typeof useRenameDataset>, 'showDatasetRenameModal'> & {
    dataset: IDataset;
  }) {
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

  if (!hasManagePermissionPermission(dataset.operator_permission)) {
    return null;
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent>
        {hasManagePermissionPermission(dataset.operator_permission) && (
          <DropdownMenuItem onClick={handleShowDatasetRenameModal}>
            {t('common.rename')} <PenLine />
          </DropdownMenuItem>
        )}

        {/* {hasManagePermissionPermission(dataset.operator_permission) && (
          <DropdownMenuItem
            onClick={(e) => {
              e.stopPropagation();
              showDatasetDuplicateModal(dataset);
            }}
          >
            {t('common.duplicate')} <LucideCopy />
          </DropdownMenuItem>
        )} */}
        {hasManagePermissionPermission(dataset.operator_permission) && (
          <>
            <DropdownMenuItem onClick={handlesShowPrivilegeModal}>
              <PrivilegeDropdown></PrivilegeDropdown>
            </DropdownMenuItem>
          </>
        )}

        {hasOwnerPermission(dataset.operator_permission) && (
          <>
            <DropdownMenuSeparator />
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
