import { PermissionResourceType, TeamRole } from '@/constants/team';

export type CollaboratorItem = {
  id: string;
  label: string;
  checked?: boolean;
  permission?: number;
  role?: TeamRole;
  avatar?: string;
};

export type CheckedCollaboratorItem = Pick<
  CollaboratorItem,
  'id' | 'checked' | 'role'
>;

export interface IPrivilegeManagementInitialValues {
  id: string;
  tenant_id: string;
  name: string;
  avatar?: string;
  icon?: string;
  resourceType?: PermissionResourceType;
  kbs?: string[];
  llm_id?: string;
  embd_id?: string; // knowledge base
  documents?: string[]; // dataset documents
}
