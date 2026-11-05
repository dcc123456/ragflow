import { useCanManageDocument } from '@/ee/pages/dataset/components/metedata/hooks/use-can-manage-document';
import { useFetchKnowledgeBaseConfiguration } from '@/hooks/use-knowledge-request';
import {
  IBuiltInMetadataItem,
  IMetaDataReturnJSONSettings,
} from '@/pages/dataset/components/metedata/interface';
import { updateDocumentMetaDataConfig } from '@/services/knowledge-service';
import { useCallback } from 'react';

export type SaveBuiltinMetadataParams = {
  kbId?: string;
  documentId?: string;
  builtInMetadata?: IBuiltInMetadataItem[];
  metadata: IMetaDataReturnJSONSettings;
};

/**
 * Persists built-in metadata for a single document, but only when the operator
 * may manage that document. Collaborators without manage rights see the
 * metadata form without the built-in values being written back.
 */
export function useSaveBuiltinMetadata() {
  const { data: knowledgeDetails } = useFetchKnowledgeBaseConfiguration();
  const canManageDocument = useCanManageDocument(
    knowledgeDetails.operator_permission,
  );

  const saveBuiltinMetadata = useCallback(
    async ({
      kbId,
      documentId,
      builtInMetadata,
      metadata,
    }: SaveBuiltinMetadataParams) => {
      if (!builtInMetadata || !documentId || !canManageDocument) {
        return;
      }

      await updateDocumentMetaDataConfig({
        kb_id: kbId || '',
        doc_id: documentId,
        data: { metadata, builtInMetadata },
      });
    },
    [canManageDocument],
  );

  return { saveBuiltinMetadata };
}
