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

export interface IShareDialog {
  failed: Failed[];
  failed_departments: Failed[];
  failed_groups: Failed[];
  successful: string[];
}

export interface Failed {
  kbs: Kb[];
  llm_factory: string;
  id: string;
  name: string;
}

interface Kb {
  id: string;
  name: string;
  type: string;
}

export type ISafeDelete = Record<string, Record<string, Resource[]>>;

interface Resource {
  id: string;
  name: string;
}
