import { TeamRole } from '@/constants/team';

export interface IPermissionManagementDialogProps {
  hideModal: () => void;
  initialValues: {
    id: string;
    name: string;
    avatar?: string;
    email?: string;
    role: TeamRole;
    tenant_id: string;
  };
}

export interface IKnowledgePermission {
  kb_id: string;
  name: string;
  avatar?: string;
  permission: number;
  module_type?: string; // 模块类型：Agent, MCP, Dataset, Chat, Model
}

export type PermissionFilter =
  | 'all'
  | 'manage'
  | 'write'
  | 'read'
  | 'invisible';
