import { PermissionResourceType } from '@/constants/team';
import { IDataset } from '@/interfaces/database/dataset';
import { useMemo } from 'react';

export function useKnowledgeWithSourceType(record: IDataset) {
  return useMemo(() => {
    return { ...record, resourceType: PermissionResourceType.KnowledgeBase };
  }, [record]);
}
