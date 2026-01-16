import { PermissionResourceType } from '@/constants/team';
import { useShowDeleteConfirm } from '@/hooks/common-hooks';
import { useUpdatePermission } from '@/hooks/use-team';
import { IPermission } from '@/interfaces/database/team';
import React from 'react';
import { IPrivilegeManagementInitialValues } from '../interface';
import { FieldMap } from './constants';

function buildListParams(records: IPermission[]) {
  return records.reduce<Record<string, string[]>>((pre, cur) => {
    const field = FieldMap[cur.role as keyof typeof FieldMap];

    if (field in pre) {
      pre[field].push(cur.id);
    } else {
      pre[field] = [cur.id];
    }

    return pre;
  }, {});
}

export function useOperatePermission({
  initialValues,
}: {
  initialValues: IPrivilegeManagementInitialValues;
}) {
  const { updatePermission } = useUpdatePermission();
  const showDeleteConfirm = useShowDeleteConfirm();

  const setPermission = React.useCallback(
    (permission: number, record: IPermission[]) => {
      const listParams = buildListParams(record);
      return updatePermission({
        resource_type:
          initialValues.resourceType || PermissionResourceType.KnowledgeBase,
        resource_ids:
          initialValues.resourceType === PermissionResourceType.Document
            ? initialValues.documents || []
            : [initialValues.id],
        tenant_id: initialValues.tenant_id,
        permission: permission,
        ...listParams,
      });
    },
    [
      initialValues.documents,
      initialValues.id,
      initialValues.resourceType,
      initialValues.tenant_id,
      updatePermission,
    ],
  );

  const handleSwitchPermission = React.useCallback(
    (permission: string, record: IPermission | Array<IPermission>) => {
      if (Array.isArray(record)) {
        setPermission(Number(permission), record);
      } else {
        setPermission(Number(permission), [record]);
      }
    },
    [setPermission],
  );

  const handleDelete = React.useCallback(
    (record: IPermission | Array<IPermission>, callback?: () => void) => () => {
      showDeleteConfirm({
        onOk: async () => {
          if (Array.isArray(record)) {
            await setPermission(0, record);
            callback?.();
          } else {
            setPermission(0, [record]);
          }
        },
      });
    },
    [setPermission, showDeleteConfirm],
  );

  return { handleSwitchPermission, handleDelete, setPermission };
}
