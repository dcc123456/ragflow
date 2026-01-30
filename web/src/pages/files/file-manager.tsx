import { BulkOperateBar } from '@/components/bulk-operate-bar';
import { FileUploadDialog } from '@/components/file-upload-dialog';
import { Button } from '@/components/ui/button';
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
import { MoveDialog } from './move-dialog';
import { SyncFileDialog } from './sync-file-dialog';
import { useBulkOperateFile } from './use-bulk-operate-file';
import { useHandleCreateFolder } from './use-create-folder';
import { useHandleMoveFile } from './use-move-file';
import { useHandleSyncFile } from './use-sync-file';
import { useHandleUploadFile } from './use-upload-file';
export enum FileTabs {
  FILE = 'file',
  EVALUATION = 'evaluation',
}
export const FilesManager = {
  root: ({
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
      handleInputChange,
    } = useFetchFileList();

    const {
      rowSelection,
      setRowSelection,
      rowSelectionIsEmpty,
      clearRowSelection,
      selectedCount,
    } = useRowSelection();

    const {
      showMoveFileModal,
      moveFileVisible,
      onMoveFileOk,
      hideMoveFileModal,
      moveFileLoading,
    } = useHandleMoveFile({ clearRowSelection });

    const { list } = useBulkOperateFile({
      files,
      rowSelection,
      showMoveFileModal,
      setRowSelection,
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
        ></FilesTable>

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
  fileUpload: ({
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
