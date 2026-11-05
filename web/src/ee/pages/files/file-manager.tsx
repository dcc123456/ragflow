import { BulkOperateBar } from '@/components/bulk-operate-bar';
import { FileUploadDialog } from '@/components/file-upload-dialog';
import { PrivilegeManagementDialog } from '@/ee/components/privilege-management/privilege-management-dialog';
import { Button } from '@/components/ui/button';
import { PermissionResourceType } from '@/ee/constants/team';
import { useClearSelectionOnPageChange } from '@/hooks/logic-hooks/use-clear-selection-on-page-change';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useRowSelection } from '@/hooks/logic-hooks/use-row-selection';
import { useFetchFileList } from '@/hooks/use-file-request';
import { IFile } from '@/interfaces/database/file-manager';
import { IPrivilegeManagementInitialValues } from '@/ee/components/privilege-management/interface';
import { Upload } from 'lucide-react';
import {
  Dispatch,
  SetStateAction,
  useCallback,
  useEffect,
  useState,
} from 'react';
import { useTranslation } from 'react-i18next';
import { FilesInstanceType } from '@/ee/pages/files/constant';
import EnterpriseFeature from '@/pages/admin/components/enterprise-feature';
import { CreateFolderDialog } from '@/pages/files/create-folder-dialog';
import { FilesTable } from '@/pages/files/files-table';
import {
  useHandleConnectToKnowledge,
  UseHandleConnectToKnowledgeReturnType,
} from '@/pages/files/hooks';
import { MoveDialog } from '@/pages/files/move-dialog';
import { SyncFileDialog } from '@/ee/pages/files/sync-file-dialog';
import { useBulkOperateFile } from '@/pages/files/use-bulk-operate-file';
import { useHandleCreateFolder } from '@/pages/files/use-create-folder';
import { useHandleMoveFile } from '@/pages/files/use-move-file';
import { useNavigateToOtherFolder } from '@/pages/files/use-navigate-to-folder';
import { useHandleUploadFile } from '@/pages/files/use-upload-file';
import { useGoToPreviousPageOnEmpty } from '@/hooks/logic-hooks';
import { ListDeletionKey } from '@/constants/list-deletion';
import { useHandleSyncFile } from '@/ee/pages/files/use-sync-file';
import { isFolderType } from '@/pages/files/util';
export const FilesManager = {
  Root: ({
    setFileInstance,
  }: {
    setFileInstance: Dispatch<SetStateAction<FilesInstanceType>>;
  }) => {
    const {
      fileUploadVisible,
      hideFileUploadModal,
      showFileUploadModal,
      fileUploadLoading,
      onFileUploadOk,
    } = useHandleUploadFile();

    const {
      folderCreateModalVisible,
      showFolderCreateModal,
      hideFolderCreateModal,
      folderCreateLoading,
      onFolderCreateOk,
    } = useHandleCreateFolder();

    const {
      syncFileVisible,
      hideSyncFileModal,
      showSyncFileModal,
      syncFileLoading,
      syncFile,
    } = useHandleSyncFile();

    const {
      pagination,
      files,
      total,
      loading,
      setPagination,
      searchString,
      setSearchString,
      handleInputChange,
      folderNotFound,
    } = useFetchFileList();

    const navigateToOtherFolder = useNavigateToOtherFolder();
    const [permissionTarget, setPermissionTarget] =
      useState<IPrivilegeManagementInitialValues | null>(null);
    const showPrivilegeModal = useCallback((record: IFile) => {
      setPermissionTarget({
        id: record.id,
        tenant_id: record.tenant_id,
        name: record.name,
        resourceType: isFolderType(record.type)
          ? PermissionResourceType.Folder
          : PermissionResourceType.File,
      });
    }, []);

    // The folder currently open was removed (e.g. an ancestor was moved into
    // another folder). Send the user back to the root instead of showing an
    // error for a listing that no longer exists.
    useEffect(() => {
      if (folderNotFound) {
        navigateToOtherFolder('');
      }
    }, [folderNotFound, navigateToOtherFolder]);

    const {
      rowSelection,
      setRowSelection,
      rowSelectionIsEmpty,
      clearRowSelection,
      selectedCount,
    } = useRowSelection();

    useClearSelectionOnPageChange(pagination, clearRowSelection);

    useGoToPreviousPageOnEmpty(files?.length, loading, {
      deletionKey: ListDeletionKey.FileList,
      searchString,
      setSearchString,
    });

    const {
      showMoveFileModal,
      moveFileVisible,
      onMoveFileOk,
      hideMoveFileModal,
      moveFileLoading,
    } = useHandleMoveFile({ clearRowSelection });

    const connectKnowledgeModal: UseHandleConnectToKnowledgeReturnType =
      useHandleConnectToKnowledge();

    const { list } = useBulkOperateFile({
      files,
      rowSelection,
      showMoveFileModal,
      setRowSelection,
      showConnectToKnowledgeModal:
        connectKnowledgeModal.showConnectToKnowledgeModal,
    });

    useEffect(() => {
      setFileInstance({
        searchString,
        onSearchChange: handleInputChange,
        showFileUploadModal,
        showSyncFileModal,
        showFolderCreateModal,
      });
    }, [
      searchString,
      showFileUploadModal,
      showSyncFileModal,
      showFolderCreateModal,
      handleInputChange,
      setFileInstance,
    ]);

    return (
      <>
        {!rowSelectionIsEmpty && (
          <BulkOperateBar list={list} count={selectedCount}></BulkOperateBar>
        )}
        <FilesTable
          files={files}
          total={total}
          pagination={pagination}
          setPagination={setPagination}
          loading={loading}
          rowSelection={rowSelection}
          setRowSelection={setRowSelection}
          showMoveFileModal={showMoveFileModal}
          connectKnowledgeModal={connectKnowledgeModal}
          showPrivilegeModal={showPrivilegeModal}
        />

        {permissionTarget && (
          <PrivilegeManagementDialog
            hideModal={() => setPermissionTarget(null)}
            initialValues={permissionTarget}
          />
        )}

        {fileUploadVisible && (
          <FileUploadDialog
            hideModal={hideFileUploadModal}
            onOk={onFileUploadOk}
            loading={fileUploadLoading}
          ></FileUploadDialog>
        )}
        {folderCreateModalVisible && (
          <CreateFolderDialog
            loading={folderCreateLoading}
            visible={folderCreateModalVisible}
            hideModal={hideFolderCreateModal}
            onOk={onFolderCreateOk}
          ></CreateFolderDialog>
        )}
        {moveFileVisible && (
          <MoveDialog
            hideModal={hideMoveFileModal}
            onOk={onMoveFileOk}
            loading={moveFileLoading}
          ></MoveDialog>
        )}
        <EnterpriseFeature>
          {() => (
            <SyncFileDialog
              visible={syncFileVisible}
              hideModal={hideSyncFileModal}
              onOk={syncFile}
              loading={syncFileLoading}
            />
          )}
        </EnterpriseFeature>
      </>
    );
  },
  FileUpload: ({
    showFileUploadModal,
    showSyncFileModal,
    showFolderCreateModal,
  }: {
    showFileUploadModal: () => void;
    showSyncFileModal: () => void;
    showFolderCreateModal: () => void;
  }) => {
    const { t } = useTranslation();
    return (
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button>
            <Upload />
            {t('knowledgeDetails.addFile')}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-56">
          <DropdownMenuItem onClick={showFileUploadModal}>
            {t('fileManager.uploadFile')}
          </DropdownMenuItem>
          <EnterpriseFeature>
            {() => (
              <DropdownMenuItem onClick={showSyncFileModal}>
                {t('fileManager.syncFile')}
              </DropdownMenuItem>
            )}
          </EnterpriseFeature>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={showFolderCreateModal}>
            {t('fileManager.newFolder')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    );
  },
};
