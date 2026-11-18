/**
 * Enterprise seam for collaborator permissions and team roles.
 *
 * The constants describe the enterprise permission bit set; open source keeps
 * them so shared code can compare against `operator_permission`, but every
 * resource is owned by its tenant, so the permission helpers treat the current
 * user as owner.
 */
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

export const PermissionLevelOptions = [
  {
    action: Permission.Read,
    value: Permission.Read,
  },
  {
    action: Permission.Write,
    value: Permission.Write,
  },
  {
    action: Permission.Manage,
    value: Permission.Manage,
  },
] as const;

export function hasResourcePermission(permission: number, action: number) {
  return (permission & action) === action;
}

export function getResourcePermissionActions(permission: number) {
  if (hasResourcePermission(permission, Permission.Owner)) {
    return [Permission.Owner];
  }
  if (hasResourcePermission(permission, Permission.Manage)) {
    return [Permission.Manage];
  }
  if (hasResourcePermission(permission, Permission.Write)) {
    return [Permission.Write];
  }
  if (hasResourcePermission(permission, Permission.Read)) {
    return [Permission.Read];
  }
  return [];
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

export function getAssignableResourcePermissionOptions(
  resourceType: PermissionResourceType | undefined,
  canGrantManage: boolean,
) {
  return PermissionLevelOptions.filter(({ action }) => {
    if (
      resourceType === PermissionResourceType.LLM ||
      resourceType === PermissionResourceType.ModelInstance
    ) {
      return action === Permission.Read;
    }
    return canGrantManage || action !== Permission.Manage;
  });
}

export enum TenantRole {
  Owner = 'owner',
  Invite = 'invite',
  Normal = 'normal',
}

/**
 * Group-management bits returned by the Tenant groups API in the numeric
 * `permission` field. They mirror `entity.GroupPermission*` in the Go backend.
 */
export enum GroupPermission {
  AddSubgroup = 1 << 0,
  DeleteSubgroup = 1 << 1,
  AddMember = 1 << 2,
  RemoveMember = 1 << 3,
  ConfigureMemberPermissions = 1 << 4,
  Owner = 1 << 5,
}

/** Wire names accepted by the group member permission update endpoint. */
export enum GroupPermissionName {
  AddSubgroup = 'add_subgroup',
  DeleteSubgroup = 'delete_subgroup',
  AddMember = 'add_member',
  RemoveMember = 'remove_member',
  ConfigureMemberPermissions = 'configure_member_permissions',
  Owner = 'owner',
}

/** Group membership states returned by the Tenant group APIs. */
export enum GroupMemberStatus {
  Removed = '0',
  Joined = '1',
  Invited = '2',
  Rejected = '3',
}
