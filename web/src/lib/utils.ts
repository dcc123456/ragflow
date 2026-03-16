import { clsx, type ClassValue } from 'clsx';
import React from 'react';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatBytes(
  bytes: number,
  opts: {
    decimals?: number;
    sizeType?: 'accurate' | 'normal';
  } = {},
) {
  const { decimals = 0, sizeType = 'normal' } = opts;

  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
  const accurateSizes = ['Bytes', 'KiB', 'MiB', 'GiB', 'TiB'];
  if (bytes === 0) return '0 Byte';
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(decimals)} ${
    sizeType === 'accurate'
      ? (accurateSizes[i] ?? 'Bytes')
      : (sizes[i] ?? 'Bytes')
  }`;
}

export const convertBytesToGb = (
  bytes: number,
  decimalPlaces: number = 2,
): number => {
  const gb = bytes / (1024 * 1024 * 1024);
  return (
    Math.round(gb * Math.pow(10, decimalPlaces)) / Math.pow(10, decimalPlaces)
  );
};

export const convertKbToGb = (
  bytes: number,
  decimalPlaces: number = 2,
): number => {
  const gb = bytes / (1024 * 1024);
  return (
    Math.round(gb * Math.pow(10, decimalPlaces)) / Math.pow(10, decimalPlaces)
  );
};
export function combineRefs<T>(...refs: React.ForwardedRef<T>[]) {
  return (node: T) => {
    refs.forEach((ref) => {
      if (typeof ref === 'function') {
        ref(node);
      } else if (ref) {
        ref.current = node;
      }
    });
  };
}
