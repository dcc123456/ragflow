import { Permission, PermissionResourceType, TeamRole } from '@/constants/team';
import {
  useFetchPermissionByTarget,
  useUpdatePermission,
} from '@/hooks/use-team';
import { useCallback, useMemo, useState } from 'react';
import { IKnowledgePermission, PermissionFilter } from './interface';

// Maps the frontend TeamRole enum to the backend target_type value.
const ROLE_TO_TARGET_TYPE: Record<TeamRole, 'member' | 'group' | 'department'> =
  {
    [TeamRole.Member]: 'member',
    [TeamRole.Group]: 'group',
    [TeamRole.Department]: 'department',
  };

export function usePermissionData(
  tenantId: string,
  targetId: string,
  role: TeamRole,
) {
  // Local overrides applied optimistically before the user hits "Confirm".
  const [localOverrides, setLocalOverrides] = useState<Record<string, number>>(
    {},
  );

  const { data: rawData, loading } = useFetchPermissionByTarget({
    tenant_id: tenantId,
    target_id: targetId,
    target_type: ROLE_TO_TARGET_TYPE[role],
  });

  // Merge server data with any pending local edits so the table stays in sync.
  const permissions: IKnowledgePermission[] = useMemo(
    () =>
      (rawData ?? []).map((item) => ({
        kb_id: item.resource_id,
        name: item.name,
        avatar: item.avatar,
        permission:
          item.resource_id in localOverrides
            ? localOverrides[item.resource_id]
            : item.permission,
        module_type: item.module_type,
      })),
    [rawData, localOverrides],
  );

  const updatePermissionLocal = useCallback(
    (kbId: string, newPermission: number) => {
      setLocalOverrides((prev) => ({ ...prev, [kbId]: newPermission }));
    },
    [],
  );

  return { permissions, loading, updatePermissionLocal };
}

export function useFilteredPermissions(
  permissions: IKnowledgePermission[],
  searchKeyword: string,
  permissionFilter: PermissionFilter,
) {
  return useMemo(() => {
    return permissions.filter((item) => {
      // Search filter
      const matchesSearch = searchKeyword
        ? item.name.toLowerCase().includes(searchKeyword.toLowerCase())
        : true;

      // Permission filter
      let matchesPermission = true;
      if (permissionFilter !== 'all') {
        switch (permissionFilter) {
          case 'manage':
            matchesPermission = item.permission === Permission.Manage;
            break;
          case 'write':
            matchesPermission = item.permission === Permission.Write;
            break;
          case 'read':
            matchesPermission = item.permission === Permission.Read;
            break;
          case 'invisible':
            matchesPermission = item.permission === 0;
            break;
        }
      }

      return matchesSearch && matchesPermission;
    });
  }, [permissions, searchKeyword, permissionFilter]);
}

export function usePermissionUpdate(
  tenantId: string,
  targetId: string,
  role: TeamRole,
) {
  const { updatePermission, loading } = useUpdatePermission();

  const handleUpdatePermission = useCallback(
    async (kbIds: string[], permission: number) => {
      const params: {
        permission: number;
        resource_type: string;
        resource_ids: string[];
        tenant_id: string;
        member_list?: string[];
        group_list?: string[];
        department_list?: string[];
      } = {
        permission,
        resource_type: PermissionResourceType.KnowledgeBase,
        resource_ids: kbIds,
        tenant_id: tenantId,
      };

      // Set the appropriate list based on role
      if (role === TeamRole.Member) {
        params.member_list = [targetId];
      } else if (role === TeamRole.Group) {
        params.group_list = [targetId];
      } else if (role === TeamRole.Department) {
        params.department_list = [targetId];
      }

      return await updatePermission(params);
    },
    [targetId, role, tenantId, updatePermission],
  );

  return { handleUpdatePermission, loading };
}
