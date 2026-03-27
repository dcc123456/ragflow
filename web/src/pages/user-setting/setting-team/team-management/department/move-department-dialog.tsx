import { PrivilegeAvatar } from '@/components/privilege-management/privilege-avatar';
import { ButtonLoading } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { TreeDataItem, TreeView } from '@/components/ui/tree-view';
import { TagRenameId } from '@/constants/knowledge';
import { useFetchDepartmentList } from '@/hooks/use-team';
import { IModalProps } from '@/interfaces/common';
import { IDepartment } from '@/interfaces/database/team';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { RootNodeId } from '../constant';
import { useTenantId } from '../use-operate-team';

function buildTree(
  initialDepartmentId: string,
  list: Array<Omit<IDepartment, 'parent_id'> & { parent_id: string | null }>,
  parentId: string | null = null,
): TreeDataItem[] {
  const li = list
    .filter(
      (x) =>
        x.parent_id === parentId && x.department_id !== initialDepartmentId,
    )
    .map((x) => ({
      id: x.department_id,
      name: x.name,
      icon: () => (
        <span className="mr-2">
          <PrivilegeAvatar avatar={x.avatar}></PrivilegeAvatar>
        </span>
      ),
      children: buildTree(initialDepartmentId, list, x.department_id),
    }));

  return li;
}

export function MoveDepartmentDialog({
  hideModal,
  onOk,
  loading,
  initialDepartmentId,
}: IModalProps<any> & { initialDepartmentId: string }) {
  const { t } = useTranslation();
  const tenantId = useTenantId();
  const { data, setFetchDepartmentListParams } =
    useFetchDepartmentList(tenantId);
  const [selectedId, setSelectedId] = useState<string>('');

  const list = useMemo(() => {
    const li = data.map((x) => ({
      ...x,
      parent_id: x.parent_id === x.department_id ? null : x.parent_id,
    }));
    return [
      {
        id: RootNodeId,
        name: 'Root',
        children: buildTree(initialDepartmentId, li),
      },
    ];
  }, [data, initialDepartmentId]);

  const handleOk = useCallback(async () => {
    if (!selectedId) {
      return; // show message
    }
    onOk?.(selectedId);
  }, [onOk, selectedId]);

  const handleSelect = useCallback((item: TreeDataItem | undefined) => {
    if (item) {
      setSelectedId(item.id);
    }
  }, []);

  useEffect(() => {
    setFetchDepartmentListParams({ parentId: '', all: true });
  }, [setFetchDepartmentListParams]);

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="max-w-4xl">
        <DialogHeader>
          <DialogTitle> {t('common.move')}</DialogTitle>
        </DialogHeader>
        <div>
          <TreeView data={list} onSelectChange={handleSelect} expandAll />
        </div>
        <DialogFooter>
          <ButtonLoading
            type="submit"
            form={TagRenameId}
            loading={loading}
            onClick={handleOk}
          >
            {t('common.save')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
