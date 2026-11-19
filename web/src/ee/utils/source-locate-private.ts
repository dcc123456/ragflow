import type { IHighlight } from 'react-pdf-highlighter';
import { isCadDocumentExtension } from '@/ee/utils/cad-document';

export function supportsAdditionalSourceLocate(extension?: string) {
  return isCadDocumentExtension(extension);
}

export function getSourceLocatePageNumber(highlights?: IHighlight[]) {
  return highlights?.[0]?.position.pageNumber ?? 1;
}
