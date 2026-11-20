/**
 * Extractor sub-task params the shared transform has no form control for.
 * transformExtractorParams spreads this over the rebuilt params so saving a
 * copied canvas keeps the sub-tasks its template configured — without it
 * the Tokenizer's `fields: "embedding_text"` would point at a field the
 * Extractor no longer produces.
 *
 * The resume four-lane extraction mode is the one such sub-task today: pass
 * the sub-object through with its enabled flag.
 */
export const extraExtractorParams = (
  raw: Record<string, any>,
): Record<string, any> => {
  if (raw?.resume === undefined) {
    return {};
  }
  return { resume: { enabled: Boolean(raw.resume?.enabled) } };
};
