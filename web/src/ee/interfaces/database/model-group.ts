export interface IModelGroup {
  id: string;
  user_id: string;
  tenant_id: string;
  nickname: string;
  group_name: string;
  status: string;
  operator_permission?: number;
}

export interface IModelGroupMember {
  id: string;
  model_id: string;
  position: number;
  provider: string;
  instance: string;
  model: string;
  model_type: string[] | null;
  status: string;
  unavailable?: boolean;
  restricted?: boolean;
  group_name: string;
  group_id: string;
}

export interface IModelGroupDetailResult {
  group: IModelGroup;
  members: IModelGroupMember[];
  isError: boolean;
}
