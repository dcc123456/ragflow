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
  Owner = 7,
}

export const PermissionList = [
  Permission.Read,
  Permission.Write,
  Permission.Manage,
];

export const LabelMap = {
  [Permission.Read]: 'read',
  [Permission.Write]: 'write',
  [Permission.Manage]: 'manage',
  [Permission.Owner]: 'owner',
};

export enum PermissionResourceType {
  KnowledgeBase = 'kb',
  LLM = 'llm',
  Dialog = 'dialog',
  Document = 'document',
  Canvas = 'canvas',
  MCP = 'mcp',
}

export enum TenantRole {
  Owner = 'owner',
  Invite = 'invite',
  Normal = 'normal',
}
