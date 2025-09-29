import { BulkOperateBar } from '@/components/bulk-operate-bar';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useRowSelection } from '@/hooks/logic-hooks/use-row-selection';
import { useFetchPermissionList } from '@/hooks/use-team';
import { IModalProps } from '@/interfaces/common';
import { useTranslation } from 'react-i18next';
import { IPrivilegeManagementInitialValues } from '../interface';
import { ManagePrivilegeTable } from './manage-privilege-table';
import { useBulkOperatePrivilege } from './use-bulk-operate-priviledge';

export function ManagePrivilegeDialog({
  hideModal,
  initialValues,
}: IModalProps<any> & { initialValues: IPrivilegeManagementInitialValues }) {
  const { t } = useTranslation();

  const { rowSelection, rowSelectionIsEmpty, setRowSelection, selectedCount } =
    useRowSelection();

  const { data } = useFetchPermissionList(
    initialValues.tenant_id,
    initialValues.id,
    initialValues.resourceType,
  );

  const { list } = useBulkOperatePrivilege({
    rowSelection,
    setRowSelection,
    initialValues,
    permissions: data,
  });

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="max-w-4xl gap-0">
        <DialogHeader className="mb-4">
          <DialogTitle>{t('permission.manageCollaborator')}</DialogTitle>
        </DialogHeader>
        {rowSelectionIsEmpty || (
          <BulkOperateBar list={list} count={selectedCount}></BulkOperateBar>
        )}
        <ManagePrivilegeTable
          initialValues={initialValues}
          rowSelection={rowSelection}
          setRowSelection={setRowSelection}
          data={data}
        ></ManagePrivilegeTable>
      </DialogContent>
    </Dialog>
  );
}
