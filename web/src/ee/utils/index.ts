/**
 * Enterprise extension of `@/utils`.
 *
 * The shared module stays byte-identical to upstream; enterprise callers import
 * this module when they need the helpers below.
 */
export * from '@/utils';

export const isLicenseError = (code?: number, message?: string): boolean =>
  code === 109 && !!message?.toLowerCase().includes('license');
