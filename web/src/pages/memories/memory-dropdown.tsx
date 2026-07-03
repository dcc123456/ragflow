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
import {
  hasManagePermissionPermission,
  hasOwnerPermission,
} from '@/utils/permission-util';
import { PenLine, Trash2 } from 'lucide-react';
import { MouseEventHandler, PropsWithChildren, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useDeleteMemory } from './hooks';
import { IMemory } from './interface';

export function MemoryDropdown({
  children,
  memory,
  showMemoryRenameModal,
  showPrivilegeModal,
}: PropsWithChildren & {
  memory: IMemory;
  showMemoryRenameModal: (memory: IMemory) => void;
  showPrivilegeModal(): void;
}) {
  const { t } = useTranslation();
  const { deleteMemory } = useDeleteMemory();
  const canManageMemory = hasManagePermissionPermission(
    memory.operator_permission ?? 0,
  );
  const canDeleteMemory = hasOwnerPermission(memory.operator_permission ?? 0);
  const handleShowChatRenameModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showMemoryRenameModal(memory);
      },
      [memory, showMemoryRenameModal],
    );
  const handleDelete: MouseEventHandler<HTMLDivElement> = useCallback(() => {
    deleteMemory({ memory_id: memory.id });
  }, [memory, deleteMemory]);

  const handlesShowPrivilegeModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showPrivilegeModal();
      },
      [showPrivilegeModal],
    );

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent>
        {canManageMemory && (
          <>
            <DropdownMenuItem onClick={handleShowChatRenameModal}>
              {t('common.rename')} <PenLine />
            </DropdownMenuItem>
            <DropdownMenuItem onClick={handlesShowPrivilegeModal}>
              <PrivilegeDropdown />
            </DropdownMenuItem>
          </>
        )}
        {canDeleteMemory && (
          <>
            <DropdownMenuSeparator />
            <ConfirmDeleteDialog
              onOk={handleDelete}
              title={t('deleteModal.delMemory')}
              content={{
                node: (
                  <ConfirmDeleteDialogNode
                    avatar={{ avatar: memory.avatar, name: memory.name }}
                    name={memory.name}
                    warnText={t('memories.delMemoryWarn')}
                  />
                ),
              }}
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
