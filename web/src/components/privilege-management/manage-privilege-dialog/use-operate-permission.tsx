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

  const togglePermission = React.useCallback(
    async (permissionBit: number, records: IPermission[]) => {
      type PermissionUpdate = {
        resourceIds: Set<string>;
        memberList: Set<string>;
        groupList: Set<string>;
        departmentList: Set<string>;
      };

      const updates = new Map<number, PermissionUpdate>();
      const resourceIds =
        initialValues.resourceType === PermissionResourceType.Document
          ? initialValues.documents || []
          : [initialValues.id];

      for (const record of records) {
        const targetField = FieldMap[record.role as keyof typeof FieldMap];
        if (!targetField) continue;
        const shouldGrant = !resourceIds.every(
          (resourceId) =>
            ((record.permissions[resourceId] ?? 0) & permissionBit) !== 0,
        );

        for (const resourceId of resourceIds) {
          const currentMask = record.permissions[resourceId] ?? 0;
          const nextMask = shouldGrant
            ? currentMask | permissionBit
            : currentMask & ~permissionBit;
          const update = updates.get(nextMask) ?? {
            resourceIds: new Set<string>(),
            memberList: new Set<string>(),
            groupList: new Set<string>(),
            departmentList: new Set<string>(),
          };
          update.resourceIds.add(resourceId);
          if (targetField === 'member_list') update.memberList.add(record.id);
          if (targetField === 'group_list') update.groupList.add(record.id);
          if (targetField === 'department_list') {
            update.departmentList.add(record.id);
          }
          updates.set(nextMask, update);
        }
      }

      for (const [permission, update] of updates) {
        const result = await updatePermission({
          resource_type:
            initialValues.resourceType || PermissionResourceType.KnowledgeBase,
          resource_ids: [...update.resourceIds],
          tenant_id: initialValues.tenant_id,
          permission,
          member_list: [...update.memberList],
          group_list: [...update.groupList],
          department_list: [...update.departmentList],
        });
        if (result !== 0) return result;
      }

      return 0;
    },
    [
      initialValues.documents,
      initialValues.id,
      initialValues.resourceType,
      initialValues.tenant_id,
      updatePermission,
    ],
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

  return { togglePermission, handleDelete, setPermission };
}
