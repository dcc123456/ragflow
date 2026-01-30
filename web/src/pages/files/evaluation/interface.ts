export interface Collection {
  create_date: string;
  create_time: number;
  created_by: string;
  description: string;
  id: string;
  name: string;
  status: number;
  tenant_id: string;
  update_date: string;
  update_time: number;
}

export interface CollectionList {
  collections: Collection[];
  total: number;
}
