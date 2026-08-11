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
import {
  hasManagePermissionPermission,
  hasOwnerPermission,
} from '@/utils/permission-util';
import { PenLine, Trash2 } from 'lucide-react';
import { MouseEventHandler, PropsWithChildren, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { ISearchAppProps, useDeleteSearch } from './hooks';

export function SearchDropdown({
  children,
  dataset,
  showSearchRenameModal,
  showPrivilegeModal,
}: PropsWithChildren & {
  dataset: ISearchAppProps;
  showSearchRenameModal: (dataset: ISearchAppProps) => void;
  showPrivilegeModal(): void;
}) {
  const { t } = useTranslation();
  const { deleteSearch } = useDeleteSearch();
  const canManageSearch = hasManagePermissionPermission(
    dataset.operator_permission ?? 0,
  );
  const canDeleteSearch = hasOwnerPermission(dataset.operator_permission ?? 0);
  const handleShowChatRenameModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showSearchRenameModal(dataset);
      },
      [dataset, showSearchRenameModal],
    );
  const handleDelete: MouseEventHandler<HTMLDivElement> = useCallback(() => {
    deleteSearch({ search_id: dataset.id });
  }, [dataset.id, deleteSearch]);
  const handleShowPrivilegeModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showPrivilegeModal();
      },
      [showPrivilegeModal],
    );

  if (!canManageSearch && !canDeleteSearch) {
    return null;
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent>
        {canManageSearch && (
          <DropdownMenuItem onClick={handleShowChatRenameModal}>
            {t('common.rename')} <PenLine />
          </DropdownMenuItem>
        )}
        {canManageSearch && (
          <DropdownMenuItem onClick={handleShowPrivilegeModal}>
            <PrivilegeDropdown />
          </DropdownMenuItem>
        )}
        {canDeleteSearch && (
          <>
            {canManageSearch && <DropdownMenuSeparator />}
            <ConfirmDeleteDialog
              onOk={handleDelete}
              title={t('deleteModal.delSearch')}
              content={{
                node: (
                  <ConfirmDeleteDialogNode
                    avatar={{ avatar: dataset.avatar, name: dataset.name }}
                    name={dataset.name}
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
