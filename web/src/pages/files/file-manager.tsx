import { BulkOperateBar } from '@/components/bulk-operate-bar';
import { FileUploadDialog } from '@/components/file-upload-dialog';
import { Button } from '@/components/ui/button';
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
import { Upload } from 'lucide-react';
import { Dispatch, SetStateAction, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { FilesInstanceType } from '.';
import EnterpriseFeature from '../admin/components/enterprise-feature';
import { CreateFolderDialog } from './create-folder-dialog';
import { FilesTable } from './files-table';
import {
  useHandleConnectToKnowledge,
  UseHandleConnectToKnowledgeReturnType,
} from './hooks';
import { MoveDialog } from './move-dialog';
import { SyncFileDialog } from './sync-file-dialog';
import { useBulkOperateFile } from './use-bulk-operate-file';
import { useHandleCreateFolder } from './use-create-folder';
import { useHandleMoveFile } from './use-move-file';
import { useNavigateToOtherFolder } from './use-navigate-to-folder';
import { useHandleUploadFile } from './use-upload-file';
import { useGoToPreviousPageOnEmpty } from '@/hooks/logic-hooks';
import { ListDeletionKey } from '@/constants/list-deletion';
import { useHandleSyncFile } from './use-sync-file';
export enum FileTabs {
  FILE = 'file',
  EVALUATION = 'evaluation',
}
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
        />

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
