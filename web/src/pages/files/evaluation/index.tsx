import { BulkOperateBar } from '@/components/bulk-operate-bar';
import { FileUploadDialog } from '@/components/file-upload-dialog';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  useCreateEvaluationCollection,
  useFetchEvaluationCollectionList,
} from '@/hooks/use-evaluation-request';
import { Upload } from 'lucide-react';
import { Dispatch, SetStateAction, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { FilesInstanceType } from '..';
import { EditNameDialog } from './edit-name-dialog';
import { EvaluationTable } from './evaluation-table';
import { useBulkOperateEvaluation } from './use-bulk-operate-evaluation';
import {
  useBulkOperateBar,
  useEvaluationOperation,
} from './use-evaluation-operation';

export const EvaluationPage = {
  Root: ({
    setFileInstance,
  }: {
    setFileInstance: Dispatch<SetStateAction<FilesInstanceType>>;
  }) => {
    const {
      selectedItem,
      editModalVisible,
      handleEdit,
      handleDelete,
      handleEditOk,
      handleView,
      setEditModalVisible,
      setSelectedItem,
    } = useEvaluationOperation();

    const {
      searchString,
      handleInputChange,
      data: evaluationData,
      pagination,
      setPagination,
      loading,
    } = useFetchEvaluationCollectionList();

    const {
      rowSelection,
      setRowSelection,
      rowSelectionIsEmpty,
      selectedCount,
      handleBulkDelete,
    } = useBulkOperateBar();

    const { list } = useBulkOperateEvaluation({
      evaluationData: evaluationData.collections || [],
      rowSelection,
      setRowSelection,
      deleteCallBack: handleBulkDelete,
    });

    const {
      fileUploadVisible,
      hideFileUploadModal,
      onFileUploadOk,
      fileUploadLoading,
      showFileUploadModal,
    } = useCreateEvaluationCollection();

    useEffect(() => {
      setFileInstance({
        searchString,
        onSearchChange: handleInputChange,
        showFileUploadModal,
      });
    }, [searchString, setFileInstance, handleInputChange, showFileUploadModal]);

    return (
      <>
        {!rowSelectionIsEmpty && (
          <BulkOperateBar list={list} count={selectedCount}></BulkOperateBar>
        )}
        {/* Evaluation Table */}
        <EvaluationTable
          data={evaluationData.collections || []}
          pagination={pagination}
          setPagination={setPagination}
          loading={loading}
          rowSelection={rowSelection}
          setRowSelection={setRowSelection}
          onEdit={handleEdit}
          onView={handleView}
          onDelete={handleDelete}
        />

        {fileUploadVisible && (
          <FileUploadDialog
            hideModal={hideFileUploadModal}
            onOk={onFileUploadOk}
            loading={fileUploadLoading}
            accept={{
              'application/vnd.ms-excel': ['.xlsx', '.xls'],
              'text/csv': ['.csv'],
            }}
          ></FileUploadDialog>
        )}

        {/* Edit Name Dialog */}
        {editModalVisible && selectedItem && (
          <EditNameDialog
            visible={editModalVisible}
            onCancel={() => {
              setEditModalVisible(false);
              setSelectedItem(null);
            }}
            onOk={handleEditOk}
            initialName={selectedItem.name}
          />
        )}
      </>
    );
  },
  FileUpload: ({
    showFileUploadModal,
  }: {
    showFileUploadModal: () => void;
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
        </DropdownMenuContent>
      </DropdownMenu>
    );
  },
};
