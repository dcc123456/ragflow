/**
 * Enterprise extension of `@/lib/utils`.
 *
 * The shared module stays byte-identical to upstream; enterprise callers import
 * this module when they need the conversion helpers below.
 */
export * from '@/lib/utils';

export const convertBytesToGb = (
  bytes: number,
  decimalPlaces: number = 2,
): number => {
  const gb = bytes / (1000 * 1000 * 1000);
  return (
    Math.round(gb * Math.pow(10, decimalPlaces)) / Math.pow(10, decimalPlaces)
  );
};

export const convertKbToGb = (
  bytes: number,
  decimalPlaces: number = 2,
): number => {
  const gb = bytes / (1000 * 1000);
  return (
    Math.round(gb * Math.pow(10, decimalPlaces)) / Math.pow(10, decimalPlaces)
  );
};
