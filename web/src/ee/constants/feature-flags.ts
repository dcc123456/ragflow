/**
 * Enterprise seam for capability flags that shared pages read at build time.
 *
 * The enterprise tree owns its own value; open source ships the public default
 * in `web/src/ee/constants/feature-flags.ts`.
 */

/**
 * Whether resources (agents, datasets, memories) expose a `permission` field
 * for choosing between private and team ownership.
 */
export const showPermissionField = false;
