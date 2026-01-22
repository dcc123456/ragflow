import {
  ColumnFilterAutoRemoveTestFn,
  FilterFn,
  Row,
  RowData,
  SortDirection,
  TransformFilterValueFn,
} from '@tanstack/react-table';
import { LucideSortAsc, LucideSortDesc } from 'lucide-react';
import { DefaultValues } from 'react-hook-form';

import { v4 as uuidV4 } from 'uuid';

type AsyncDefaultValues<TValues> = (payload?: unknown) => Promise<TValues>;

type UUID = `${string}-${string}-${string}-${string}-${string}`;

let uuid = (): UUID => {
  /**
   * Crypto.prototype.randomUUID()
   * Secure contexts (HTTPS)
   * Baseline 2022
   */
  if (window.crypto && typeof window.crypto.randomUUID === 'function') {
    uuid = () => window.crypto.randomUUID();
  }
  // Insecure contexts, bring our own implementation
  else if (
    window.crypto &&
    typeof window.crypto.getRandomValues === 'function'
  ) {
    /**
     * Based on the section of `Crypto.prototype.randomUUID()`
     * @see https://w3c.github.io/webcrypto/#Crypto-method-randomUUID
     */
    const getData = () => {
      const data = [
        window.crypto.getRandomValues(new Uint8Array(4)),
        window.crypto.getRandomValues(new Uint8Array(2)),
        window.crypto.getRandomValues(new Uint8Array(2)),
        window.crypto.getRandomValues(new Uint8Array(2)),
        window.crypto.getRandomValues(new Uint8Array(6)),
      ];

      // Set the version bits to 0b0100xxxx (RFC 4122)
      data[2][0] = (data[2][0] & 0xf) | 0x40;
      // Set the variant bits to 0b10xxxxxx (RFC 4122)
      data[3][0] = (data[3][0] & 0x3f) | 0x80;

      return data;
    };

    /**
     * Unit8Array.prototype.toHex()
     * Baseline 2025
     * @see https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Uint8Array/toHex
     */
    // @ts-ignore
    if (typeof Uint8Array.prototype.toHex === 'function') {
      uuid = () =>
        getData()
          // @ts-ignore
          .map((d) => d.toHex())
          .join('-') as UUID;
    } else {
      uuid = () =>
        getData()
          .map((d) =>
            [...d].map((n) => n.toString(16).padStart(2, '0')).join(''),
          )
          .join('-') as UUID;
    }
  }
  // Fallback to uuid library
  else {
    uuid = () => uuidV4() as UUID;
  }

  return uuid();
};

export { uuid };

export function uuidParts() {
  return uuid().split('-') as [string, string, string, string, string];
}

export function formMergeDefaultValues<T>(
  ...parts: (DefaultValues<T> | AsyncDefaultValues<T> | undefined)[]
): (payload?: unknown) => Promise<Required<T>> {
  return async (payload?: unknown) => {
    if (parts.length === 0) {
      return {} as DefaultValues<T>;
    }

    if (parts.length === 1) {
      return typeof parts[0] === 'function'
        ? await parts[0](payload)
        : parts[0];
    }

    return Object.assign(
      // @ts-ignore
      ...(await Promise.all(
        parts.map((p) => (typeof p === 'function' ? p(payload) : p)),
      )),
    );
  };
}

export function parseBooleanish(value: any): boolean {
  return typeof value === 'string'
    ? /^(1|[Tt]rue|[Oo]n|[Yy](es)?)$/.test(value)
    : !!value;
}

export function createFuzzySearchFn<TData extends RowData>(
  columns: (keyof TData)[] = [],
) {
  return (row: Row<TData>, columnId: string, filterValue: string) => {
    const searchText = filterValue.trim().toLowerCase();

    return columns
      .map((n) =>
        row
          .getValue<string>(n as string)
          .trim()
          .toLowerCase(),
      )
      .some((v) => v.includes(searchText));
  };
}

export function createColumnFilterFn<TData extends RowData>(
  filterFn: FilterFn<TData>,
  options: {
    resolveFilterValue?: TransformFilterValueFn<TData>;
    autoRemove?: ColumnFilterAutoRemoveTestFn<TData>;
  },
) {
  return Object.assign(filterFn, options) as FilterFn<TData>;
}

export function getSortIcon(sorting: false | SortDirection) {
  return {
    asc: <LucideSortAsc />,
    desc: <LucideSortDesc />,
  }[sorting as string];
}

export const PERMISSION_TYPES = ['enable', 'read', 'write', 'share'] as const;
export const EMPTY_DATA = Object.freeze<any[]>([]) as any[];

/**
 * Get environment variable from bundler.
 */
function getEnv(key: string): string | undefined {
  return (
    import.meta.env[`VITE_${key}`] || import.meta.env[`UMI_APP_${key}`] // `UMI_APP_*` is for backward compatibility
  );
}

export const IS_ENTERPRISE =
  getEnv('RAGFLOW_ENTERPRISE') === 'RAGFLOW_ENTERPRISE';
export const USE_LDAP = getEnv('RAGFLOW_USE_LDAP') === 'RAGFLOW_USE_LDAP';
