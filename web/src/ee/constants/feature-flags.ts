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

/**
 * Whether the ingestion (dataflow) canvas exposes the Run (debug) entry.
 * The agent canvas is unaffected and always keeps its Run button.
 */
export const showPipelineRunButton = true;
