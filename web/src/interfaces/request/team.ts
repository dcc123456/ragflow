export interface ICreateDepartmentRequestBody {
  name: string;
  avatar?: string;
  parent_id?: string;
  description?: string;
}

export interface IUpdateDepartmentMemberListRequest {
  member_id: string;
  role: string;
  status?: string; // "0" or "1"
}

export interface IUpdateDepartmentRequestBody {
  department_id: string;
  parent_id?: string;
  name?: string;
  avatar?: string;
  description?: string;
  status?: string;
  member_list?: Array<IUpdateDepartmentMemberListRequest>;
}

export interface ICreateGroupRequestBody {
  name: string;
  avatar?: string;
}

export interface IUpdateGroupRequestBody {
  group_id: string;
  name?: string;
  avatar?: string;
  status?: string;
  member_list?: Array<IUpdateDepartmentMemberListRequest>;
}

export interface ITransferGroupOwnerRequestBody {
  group_id: string;
  new_owner_id: string;
  remain_admin?: boolean;
}

export interface IFetchDepartmentRequestParams {
  parentId: string;
  all?: boolean;
}

export interface IMoveDepartmentRequestBody {
  departmentId: string;
  parentId: string;
}

export interface IDeleteDepartmentMemberRequestBody {
  department_id: string;
  member_list?: Array<IUpdateDepartmentMemberListRequest>;
}

export interface IUpdatePermission {
  permission: number;
  resource_type: string;
  resource_ids: string[];
  tenant_id: string;
  member_list?: string[];
  group_list?: string[];
  department_list?: string[];
}

export interface IUpdateDialogPermission extends Omit<
  IUpdatePermission,
  'resource_ids'
> {
  kbs: string[];
  llm_factory: string;
  resource_id: string;
}

export interface IConfirmDeletePermission {
  resource_type: string;
  resource_ids: string[];
  tenant_id: string;
}

export interface IListPermissionByTargetRequest {
  tenant_id: string;
  target_id: string;
  target_type: 'member' | 'group' | 'department';
}
