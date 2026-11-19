export interface ICreateModelGroupRequest {
  name: string;
}

export interface IUpdateModelGroupMemberStatusRequest {
  composite_model_name: string;
  status: string;
}

export interface IAddModelToGroupRequest {
  model_id: string;
}

export interface IRemoveModelFromGroupRequest {
  composite_model_name?: string;
  model_id?: string;
}

export interface IReorderModelGroupMembersRequest {
  model_ids: string[];
}
