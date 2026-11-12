export type PermissionType = 'enable' | 'read' | 'write' | 'share';

export type RoleResourceName =
  | 'dataset'
  | 'chat'
  | 'agent'
  | 'search'
  | 'file'
  | 'group'
  | 'memory'
  | 'model_provider'
  | 'mcp'
  | 'invitation';

const rolePermissions = {
  getRolePermissionTypes(resourceName: string): PermissionType[] | undefined {
    if (resourceName === 'invitation' || resourceName === 'model_provider') {
      return ['enable'];
    }
    return undefined;
  },
};

export default rolePermissions;
