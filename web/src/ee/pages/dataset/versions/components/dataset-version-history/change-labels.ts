/**
 * Labels of the change set, shared by the detail panel and the comparison panel so
 * an entity or an action reads the same in both. Keys the server adds later fall
 * back to their raw value at the call site.
 */
export const CHANGE_ENTITY_LABELS: Record<string, string> = {
  document: 'datasetVersion.changeEntityDocument',
  chunk: 'datasetVersion.changeEntityChunk',
  // A compiled wiki page: neither a document nor a chunk, and named by its page
  // id so the entry points at the same thing the page history does.
  artifact: 'datasetVersion.changeEntityArtifact',
};

export const CHANGE_ACTION_LABELS: Record<string, string> = {
  add: 'datasetVersion.changeActionAdd',
  update: 'datasetVersion.changeActionUpdate',
  rename: 'datasetVersion.changeActionRename',
  delete: 'datasetVersion.changeActionDelete',
  parse: 'datasetVersion.changeActionParse',
  restore: 'datasetVersion.changeActionRestore',
  // The two directions of a visibility switch, for a document and for a chunk
  // alike: "Enabled document a.pdf", "Disabled chunk X of document Y".
  enable: 'datasetVersion.changeActionEnable',
  disable: 'datasetVersion.changeActionDisable',
};

// A chunk entry reads its actions differently where the action word alone would
// be vague: a chunk's text was edited, and the document-level "Changed" belongs
// to the document entries. Anything not listed falls back to the shared label.
export const CHUNK_ACTION_LABELS: Record<string, string> = {
  update: 'datasetVersion.changeActionChunkEdit',
};
