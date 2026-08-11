/*
 *  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

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
  hasPreviewPermission,
} from '@/utils/permission-util';
import { Ellipsis, Eye, PenLine, Trash2, Upload } from 'lucide-react';
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

  const isReadOnly = hasPreviewPermission(mcp.operator_permission);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Ellipsis className="size-5 cursor-pointer p-1 rounded-sm hover:text-text-primary hover:bg-bg-card" />
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem
          onClick={showEditModal(mcp.id, mcp.operator_permission)}
        >
          {isReadOnly ? (
            <>
              {t('common.view')} <Eye className="size-4" />
            </>
          ) : (
            <>
              {t('common.edit')} <PenLine className="size-4" />
            </>
          )}
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
