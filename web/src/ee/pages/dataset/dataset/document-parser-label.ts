import i18n from '@/locales/config';

const EnterpriseParserLabelKeys: Record<string, string> = {
  cad: 'flow.fileFormatOptions.cad',
  visio: 'flow.fileFormatOptions.visio',
};

export function getEnterpriseDocumentParserLabel(
  chunkMethod: string,
): string | undefined {
  const key = EnterpriseParserLabelKeys[chunkMethod];
  return key ? i18n.t(key) : undefined;
}
