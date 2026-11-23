const VisioDocumentExtensions = new Set(['vsdx']);

export function isVisioDocumentExtension(extension?: string) {
  return !!extension && VisioDocumentExtensions.has(extension.toLowerCase());
}
