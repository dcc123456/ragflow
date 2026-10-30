import message from '@/components/ui/message';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import { useRowSelection } from '@/hooks/logic-hooks/use-row-selection';
import {
  useDeleteEvaluationCollection,
  useUpdateEvaluationCollection,
} from '@/hooks/use-evaluation-request';
import { useState } from 'react';

export const useEvaluationOperation = () => {
  const [editModalVisible, setEditModalVisible] = useState(false);
  const [selectedItem, setSelectedItem] = useState<any>(null);
  const { navigateToFileManagerEvaluationDetail } = useNavigatePage();
  const { deleteEvaluationCollection } = useDeleteEvaluationCollection();
  const { updateEvaluationCollection } = useUpdateEvaluationCollection();
  const handleEdit = (item: any) => {
    setSelectedItem(item);
    setEditModalVisible(true);
  };

  const handleView = (item: any) => {
    // Navigate to secondary page for details
    // navigate(`${Routes.Files}${PrivateRoutes.EvaluationDetail}/${item.id}`);
    navigateToFileManagerEvaluationDetail(item.id);
  };

  const handleDelete = async (item: any) => {
    // Delete single item
    try {
      await deleteEvaluationCollection(item.id);
      // The success message is handled in the hook itself
    } catch (error) {
      message.error('Failed to delete evaluation dataset');
      console.error('Error deleting evaluation dataset:', error);
    }
  };

  const handleEditOk = async (newName: string) => {
    // Update item name
    console.log('Updating item name:', selectedItem.id, 'to', newName);
    await updateEvaluationCollection({
      collectionId: selectedItem.id,
      name: newName,
    });
    setEditModalVisible(false);
    setSelectedItem(null);
  };

  return {
    selectedItem,
    editModalVisible,
    handleEdit,
    handleDelete,

    handleEditOk,
    handleView,
    setEditModalVisible,
    setSelectedItem,
  };
};

export const useBulkOperateBar = () => {
  const { rowSelection, setRowSelection, rowSelectionIsEmpty, selectedCount } =
    useRowSelection();
  const { deleteEvaluationCollection } = useDeleteEvaluationCollection();
  const handleBulkDelete = async (ids: string[]) => {
    // Delete selected items
    console.log('Deleting selected items:', Object.keys(rowSelection));
    await deleteEvaluationCollection(ids);
  };
  return {
    rowSelection,
    setRowSelection,
    rowSelectionIsEmpty,
    selectedCount,
    handleBulkDelete,
  };
};
