/**
 * Display helper shared by the version histories (chunk and dataset).
 *
 * A version row stores the actor's user id; the server resolves it to a nickname
 * on read. A row the system wrote (a parser baseline, a delete marker) has no
 * user behind its id, so it gets a label of its own instead of the raw "system"
 * string, and an id whose user no longer exists is shown as it is.
 */
export const versionActorLabel = (
  version: { created_by?: string; created_by_name?: string },
  systemLabel: string,
) => {
  const name = version.created_by_name?.trim();
  if (name) {
    return name;
  }
  const id = version.created_by?.trim();
  if (!id) {
    return '';
  }
  return id === 'system' ? systemLabel : id;
};
