import { Permission, PermissionResourceType, TeamRole } from '@/constants/team';
import { useUpdatePermission } from '@/hooks/use-team';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { IKnowledgePermission, PermissionFilter } from './interface';

// 模块类型列表
const moduleTypes = ['Agent', 'MCP', 'Dataset', 'Chat', 'Model'];

// Mock knowledge base data
const mockKnowledgeBases = [
  { id: 'kb-001', name: 'Myrtie Nash', avatar: '' },
  { id: 'kb-002', name: 'ICBC data demo 0918 for test', avatar: '' },
  { id: 'kb-003', name: 'Gene Dennis', avatar: '' },
  { id: 'kb-004', name: 'Myrtie Nash', avatar: '' },
  { id: 'kb-005', name: 'Gene Dennis', avatar: '' },
  { id: 'kb-006', name: 'Project Documentation', avatar: '' },
  { id: 'kb-007', name: 'Customer Support KB', avatar: '' },
  { id: 'kb-008', name: 'Internal Wiki', avatar: '' },
];

// Generate mock permissions with module types
const generateMockPermissions = (
  targetId: string,
  role: TeamRole,
): IKnowledgePermission[] => {
  return mockKnowledgeBases.map((kb, index) => {
    // Generate deterministic mock permission based on index
    const permissions = [
      Permission.Manage,
      Permission.Write,
      Permission.Read,
      0,
    ];
    const mockPermission = permissions[index % 4];
    // Generate deterministic module type based on index
    const moduleType = moduleTypes[index % moduleTypes.length];

    return {
      kb_id: kb.id,
      name: kb.name,
      avatar: kb.avatar,
      permission: mockPermission,
      module_type: moduleType,
    };
  });
};

export function usePermissionData(
  tenantId: string,
  targetId: string,
  role: TeamRole,
) {
  const [permissions, setPermissions] = useState<IKnowledgePermission[]>([]);
  const [loading, setLoading] = useState(true);

  // Simulate API call to fetch permissions
  useEffect(() => {
    if (targetId) {
      setLoading(true);
      // Simulate network delay
      const timer = setTimeout(() => {
        const mockPermissions = generateMockPermissions(targetId, role);
        setPermissions(mockPermissions);
        setLoading(false);
      }, 300);

      return () => clearTimeout(timer);
    }
  }, [targetId, role]);

  const updatePermissionLocal = useCallback(
    (kbId: string, newPermission: number) => {
      setPermissions((prev) =>
        prev.map((item) =>
          item.kb_id === kbId ? { ...item, permission: newPermission } : item,
        ),
      );
    },
    [],
  );

  return {
    permissions,
    loading,
    updatePermissionLocal,
  };
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
