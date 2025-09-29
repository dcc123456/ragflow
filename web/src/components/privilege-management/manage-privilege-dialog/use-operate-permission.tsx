import { DeletePrivilegeConfirmContent } from '@/components/privilege/delete-privilege-confirm-content';
import { PermissionResourceType } from '@/constants/team';
import { useShowDeleteConfirm } from '@/hooks/common-hooks';
import { useUpdatePermission } from '@/hooks/use-team';
import { IPermission } from '@/interfaces/database/team';
import { pick } from 'lodash';
import React from 'react';
import { IPrivilegeManagementInitialValues } from '../interface';
import { FieldMap } from './constants';

export function useOperatePermission({
  initialValues,
}: {
  initialValues: IPrivilegeManagementInitialValues;
}) {
  const { updatePermission } = useUpdatePermission();
  const showDeleteConfirm = useShowDeleteConfirm();

  const setPermission = React.useCallback(
    (permission: number, record: IPermission) => {
      return updatePermission({
        resource_type:
          initialValues.resourceType || PermissionResourceType.KnowledgeBase,
        resource_id: initialValues.id,
        tenant_id: initialValues.tenant_id,
        permission: permission,
        [FieldMap[record.role as keyof typeof FieldMap]]: [record.id],
      });
    },
    [
      initialValues.id,
      initialValues.resourceType,
      initialValues.tenant_id,
      updatePermission,
    ],
  );

  const handleSwitchPermission = React.useCallback(
    (permission: string, record: IPermission | Array<IPermission>) => {
      if (Array.isArray(record)) {
        record.forEach((x) => {
          setPermission(Number(permission), x);
        });
      } else {
        setPermission(Number(permission), record);
      }
    },
    [setPermission],
  );

  const handleDelete = React.useCallback(
    (record: IPermission | Array<IPermission>, callback?: () => void) => () => {
      showDeleteConfirm({
        content: Array.isArray(record) ? null : (
          <DeletePrivilegeConfirmContent
            params={{
              ...pick(record, ['tenant_id', 'resource_type']),
              resource_ids: [record.resource_id],
            }}
          ></DeletePrivilegeConfirmContent>
        ),
        onOk: async () => {
          if (Array.isArray(record)) {
            await Promise.all(record.map((x) => setPermission(0, x)));
            callback?.();
          } else {
            setPermission(0, record);
          }
        },
      });
    },
    [setPermission, showDeleteConfirm],
  );

  return { handleSwitchPermission, handleDelete, setPermission };
}
