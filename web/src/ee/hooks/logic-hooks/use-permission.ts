import {
  hasManagementPermission,
  hasOwnerPermission,
  hasPreviewPermission,
} from '@/ee/utils/permission-util';
import { useFetchKnowledgeBaseConfiguration } from '@/hooks/use-knowledge-request';

export function useDatasetEditButtonDisabled() {
  const { data: knowledgeDetails } = useFetchKnowledgeBaseConfiguration();
  return hasPreviewPermission(knowledgeDetails.operator_permission);
}

export function useDatasetManageButtonDisabled() {
  const { data: knowledgeDetails } = useFetchKnowledgeBaseConfiguration();
  return (
    !hasManagementPermission(knowledgeDetails.operator_permission) &&
    !hasOwnerPermission(knowledgeDetails.operator_permission)
  );
}
