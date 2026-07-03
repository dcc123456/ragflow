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
import { useDeleteMcpServer } from '@/hooks/use-mcp-request';
import { IMcpServer } from '@/interfaces/database/mcp';
import {
  hasManagePermissionPermission,
  hasOwnerPermission,
  showEditButton,
} from '@/utils/permission-util';
import { Ellipsis, PenLine, Trash2, Upload } from 'lucide-react';
import { MouseEventHandler, PropsWithChildren, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useEditMcp } from './use-edit-mcp';
import { useExportMcp } from './use-export-mcp';

type McpOperationProps = {
  mcp: IMcpServer;
  showEditModal: ReturnType<typeof useEditMcp>['showEditModal'];
  showPrivilegeModal(): void;
};

export function McpOperation({
  mcp,
  showEditModal,
  showPrivilegeModal,
}: PropsWithChildren<McpOperationProps>) {
  const { t } = useTranslation();
  const { deleteMcpServer } = useDeleteMcpServer();
  const { handleExportMcpJson } = useExportMcp(mcp);

  const handleDelete: MouseEventHandler<HTMLDivElement> = useCallback(() => {
    deleteMcpServer([mcp.id]);
  }, [deleteMcpServer, mcp.id]);

  const handlesShowPrivilegeModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showPrivilegeModal();
      },
      [showPrivilegeModal],
    );

  if (!showEditButton(mcp.operator_permission)) {
    return null;
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Ellipsis className="size-5 cursor-pointer p-1 rounded-sm hover:text-text-primary hover:bg-bg-card" />
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem onClick={showEditModal(mcp.id)}>
          {t('common.edit')} <PenLine className="size-4" />
        </DropdownMenuItem>
        {hasManagePermissionPermission(mcp.operator_permission) && (
          <DropdownMenuItem onClick={handlesShowPrivilegeModal}>
            <PrivilegeDropdown />
          </DropdownMenuItem>
        )}
        {hasOwnerPermission(mcp.operator_permission) && (
          <>
            <DropdownMenuItem onClick={handleExportMcpJson([mcp.id])}>
              {t('mcp.export')} <Upload className="size-4" />
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <ConfirmDeleteDialog
              onOk={handleDelete}
              title={t('common.delete') + ' ' + t('mcp.mcpServer')}
              content={{
                node: <ConfirmDeleteDialogNode name={mcp.name} />,
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
                {t('common.delete')} <Trash2 className="size-4" />
              </DropdownMenuItem>
            </ConfirmDeleteDialog>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
