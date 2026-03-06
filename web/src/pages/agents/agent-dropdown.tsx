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
import { useDeleteAgent } from '@/hooks/use-agent-request';
import { IFlow } from '@/interfaces/database/agent';
import {
  hasManagePermissionPermission,
  hasOwnerPermission,
  showEditButton,
} from '@/utils/permission-util';
import { PenLine, Trash2 } from 'lucide-react';
import { MouseEventHandler, PropsWithChildren, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useRenameAgent } from './use-rename-agent';

export function AgentDropdown({
  children,
  showAgentRenameModal,
  agent,
  showPrivilegeModal,
}: PropsWithChildren &
  Pick<ReturnType<typeof useRenameAgent>, 'showAgentRenameModal'> & {
    agent: IFlow;
    showPrivilegeModal(): void;
  }) {
  const { t } = useTranslation();
  const { deleteAgent } = useDeleteAgent();

  const handleShowAgentRenameModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showAgentRenameModal(agent);
      },
      [agent, showAgentRenameModal],
    );

  const handleDelete: MouseEventHandler<HTMLDivElement> = useCallback(() => {
    deleteAgent([agent.id]);
  }, [agent.id, deleteAgent]);

  const handlesShowPrivilegeModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showPrivilegeModal();
      },
      [showPrivilegeModal],
    );

  if (!showEditButton(agent.operator_permission)) {
    return null;
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem onClick={handleShowAgentRenameModal}>
          {t('common.rename')} <PenLine />
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {hasManagePermissionPermission(agent.operator_permission) && (
          <>
            <DropdownMenuItem onClick={handlesShowPrivilegeModal}>
              <PrivilegeDropdown />
            </DropdownMenuItem>
            <DropdownMenuSeparator />
          </>
        )}
        {hasOwnerPermission(agent.operator_permission) && (
          <ConfirmDeleteDialog
            onOk={handleDelete}
            title={t('deleteModal.delAgent')}
            content={{
              node: (
                <ConfirmDeleteDialogNode
                  avatar={{ avatar: agent.avatar, name: agent.title }}
                  name={agent.title}
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
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
