export enum TeamRole {
  Member = 'member',
  Department = 'department',
  Group = 'group',
}

export const TeamRoleList = [
  TeamRole.Department,
  TeamRole.Member,
  TeamRole.Group,
];

export enum Permission {
  Read = 1,
  Write = 2,
  Manage = 4,
  Owner = 8,
}

export const PermissionList = [
  Permission.Read,
  Permission.Write,
  Permission.Manage,
];

export function hasResourcePermission(permission: number, action: Permission) {
  return (permission & action) === action;
}

export function getResourcePermissionActions(permission: number) {
  if (hasResourcePermission(permission, Permission.Owner)) {
    return [Permission.Owner];
  }
  return PermissionList.filter((action) =>
    hasResourcePermission(permission, action),
  );
}

export function hasResourceOwnerPermission(
  permissions?: Record<string, number>,
) {
  return Object.values(permissions ?? {}).some((permission) =>
    hasResourcePermission(permission, Permission.Owner),
  );
}

export const LabelMap = {
  [Permission.Read]: 'read',
  [Permission.Write]: 'write',
  [Permission.Manage]: 'manage',
  [Permission.Owner]: 'owner',
};

export enum PermissionResourceType {
  KnowledgeBase = 'kb',
  LLM = 'llm',
  Search = 'search',
  Dialog = 'dialog',
  Document = 'document',
  Canvas = 'canvas',
  Memory = 'memory',
  MCP = 'mcp',
  File = 'file',
  Folder = 'folder',
  ModelInstance = 'model_instance',
}

export enum TenantRole {
  Owner = 'owner',
  Invite = 'invite',
  Normal = 'normal',
}
