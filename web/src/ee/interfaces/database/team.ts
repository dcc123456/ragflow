export interface IDepartment {
  avatar?: string;
  delta_seconds: number;
  department_id: string;
  name: string;
  owner_id: string;
  role: string;
  tenant_id: string;
  update_date: string;
  parent_id: string;
  description?: string;
}

export interface IGroup {
  avatar: string;
  delta_seconds: number;
  group_id: string;
  name: string;
  owner_id: string;
  tenant_id: string;
  update_date: string;
  owner_name: string;
  members: Array<IMember>;
}

export interface ITenantGroup {
  id: string;
  name?: string;
  description?: string;
  avatar?: string;
  owner_id: string;
  owner_user_id?: string;
  tenant_id: string;
  parent_id?: string;
  path?: string;
  formatted_path?: string;
  status: string;
  is_owner: boolean;
  is_member: boolean;
  permission: number;
  permissions: string[];
  member_count: number;
}

export interface ITenantGroupMember {
  member_id: string;
  user_id: string;
  nickname: string;
  email: string;
  avatar?: string;
  status: string;
  tenant_status: string;
  is_owner: boolean;
  permission: number;
  permissions: string[];
}

export interface IMember {
  avatar?: string;
  email: string;
  is_active: string;
  is_anonymous: string;
  is_authenticated: string;
  is_superuser: boolean;
  member_id: string;
  nickname: string;
  role: string;
  status: string;
  update_date: string;
  user_id: string;
}

export interface ITenantRole {
  id: number;
  role_name: string;
  description?: string;
  is_default: boolean;
}

export interface ITenantRoleResource {
  resource_type: string;
  actions: string[];
}

export type ITenantRolePermissionMatrix = Record<
  string,
  Record<string, boolean>
>;

export interface ITenantRolePermissions {
  role: ITenantRole;
  permissions: ITenantRolePermissionMatrix;
}

export type TenantRoleDefaultModelType =
  | 'chat'
  | 'embedding'
  | 'rerank'
  | 'asr'
  | 'vision'
  | 'tts'
  | 'ocr'
  | 'decision'
  | 'scoring';

export interface ITenantRoleDefaultModel {
  role_id: number;
  model_type: TenantRoleDefaultModelType;
  model_id: string;
}

export interface ITenantRoleDefaultModels {
  model_list: ITenantRoleDefaultModel[];
  setup_status: 'complete' | 'partial' | 'not_set';
}

export interface ITenantMemberRole {
  user_id: string;
  role: string;
  role_id?: number;
  role_name?: string;
  owner?: boolean;
}

export interface ITenantPermissions {
  tenant_id: string;
  user_id: string;
  membership_role: string;
  role_id?: number;
  role_name?: string;
  permissions: Record<string, Record<string, boolean>>;
  tenant_permissions: {
    manage_members: boolean;
    manage_roles: boolean;
    owner: boolean;
  };
}

export interface ITenantManagementGrant {
  subject_type: 'user' | 'group';
  user_id?: string;
  group_id?: string;
  group_name?: string;
  permission: number;
  permissions: Array<'manage_members' | 'manage_roles'>;
}

export interface ITenantInviteLink {
  token: string;
  tenant_id: string;
  expires_at: string;
}

export interface ITenantInviteLinkInfo {
  tenant_id: string;
  tenant_name: string;
  inviter: string;
  expires_at: string;
}

export interface ITenantInviteLinkMembership {
  status: string;
}

export interface IPermission {
  id: string;
  permissions: Record<string, number>;
  resource_id: string;
  resource_type: string;
  role: string;
  tenant_id: string;
  name: string;
  avatar?: string;
}

export type ISafeDelete = Record<string, Record<string, Resource[]>>;

interface Resource {
  id: string;
  name: string;
}
