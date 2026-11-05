import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { McpOperationGuard } from '@/ee/pages/user-setting/mcp/mcp-operation-guard';
import { useDeleteMcpServer } from '@/hooks/use-mcp-request';
import { IMcpServer } from '@/interfaces/database/mcp';
import { UseEditMcpReturnType } from '@/pages/user-setting/mcp/use-edit-mcp';
import { useExportMcp } from '@/pages/user-setting/mcp/use-export-mcp';
import { Ellipsis, Eye, PenLine, Trash2, Upload } from 'lucide-react';
import { MouseEventHandler, PropsWithChildren, useCallback } from 'react';
import { useTranslation } from 'react-i18next';

export type McpOperationShellProps = PropsWithChildren<
  { mcp: IMcpServer } & Pick<UseEditMcpReturnType, 'showEditModal'> & {
      showPrivilegeModal(): void;
    }
>;

/**
 * Collaborator-aware MCP server action menu. The menu entries are gated by the
 * operator permission so a preview-only collaborator sees a view action, a
 * manager can open the permission dialog and only the owner can export or
 * delete the server.
 */
export function McpOperationShell({
  mcp,
  showEditModal,
  showPrivilegeModal,
}: McpOperationShellProps) {
  const { t } = useTranslation();
  const { deleteMcpServer } = useDeleteMcpServer();
  const { handleExportMcpJson } = useExportMcp(mcp);

  const handleDelete: MouseEventHandler<HTMLDivElement> = useCallback(() => {
    deleteMcpServer([mcp.id]);
  }, [deleteMcpServer, mcp.id]);

  const handleShowEditModal = showEditModal(mcp.id, mcp.operator_permission);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Ellipsis className="size-5 cursor-pointer p-1 rounded-sm hover:text-text-primary hover:bg-bg-card" />
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <McpOperationGuard
          mcp={mcp}
          showPrivilegeModal={showPrivilegeModal}
          editItems={
            <DropdownMenuItem onClick={handleShowEditModal}>
              {t('common.edit')} <PenLine className="size-4" />
            </DropdownMenuItem>
          }
          viewItems={
            <DropdownMenuItem onClick={handleShowEditModal}>
              {t('common.view')} <Eye className="size-4" />
            </DropdownMenuItem>
          }
          ownerItems={
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
          }
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
