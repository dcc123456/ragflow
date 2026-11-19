import i18n from '@/locales/config';

export function getEnterpriseDocumentParserLabel(
  chunkMethod: string,
): string | undefined {
  if (chunkMethod !== 'cad') {
    return undefined;
  }
  return i18n.t('flow.fileFormatOptions.cad');
}
