export interface IModelGroup {
  id: string;
  tenant_id: string;
  group_name: string;
  status: string;
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
  group_name: string;
  group_id: string;
}

export interface IModelGroupDetailResult {
  group: IModelGroup;
  members: IModelGroupMember[];
  isError: boolean;
}
