export interface ICreateModelGroupRequest {
  name: string;
}

export interface IAddModelToGroupRequest {
  model_id: string;
}

export interface IRemoveModelFromGroupRequest {
  composite_model_name: string;
}
