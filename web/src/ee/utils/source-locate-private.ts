import { isCadDocumentExtension } from '@/ee/utils/cad-document';
import { isVisioDocumentExtension } from '@/ee/utils/visio-document';
import type { IHighlight } from 'react-pdf-highlighter';

export function supportsAdditionalSourceLocate(extension?: string) {
  return (
    isCadDocumentExtension(extension) || isVisioDocumentExtension(extension)
  );
}

export function getSourceLocatePageNumber(highlights?: IHighlight[]) {
  return highlights?.[0]?.position.pageNumber ?? 1;
}
