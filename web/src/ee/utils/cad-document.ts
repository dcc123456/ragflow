const CadDocumentExtensions = new Set(['dwg', 'dxf', 'dxfb']);

export function isCadDocumentExtension(extension?: string) {
  return !!extension && CadDocumentExtensions.has(extension.toLowerCase());
}
