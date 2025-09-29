import { PermissionResourceType } from '@/constants/team';
import { IKnowledge } from '@/interfaces/database/knowledge';
import { useMemo } from 'react';

export function useKnowledgeWithSourceType(record: IKnowledge) {
  return useMemo(() => {
    return { ...record, resourceType: PermissionResourceType.KnowledgeBase };
  }, [record]);
}
