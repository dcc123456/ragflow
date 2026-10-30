// Utility functions for Model Usage Statistics page

import type { Row } from '@tanstack/react-table';
import { LucideArrowDown, LucideArrowUp, LucideTrendingUp } from 'lucide-react';

/**
 * Format number with K/M suffix
 */
export function formatNumber(num: number): string {
  if (num >= 1000000) {
    return `${parseFloat((num / 1000000).toFixed(1))}M`;
  }
  if (num >= 1000) {
    return `${parseFloat((num / 1000).toFixed(1))}K`;
  }
  return num.toString();
}

/**
 * Get rank badge variant based on rank
 */
export function getRankBadgeVariant(
  rank: number,
): 'default' | 'secondary' | 'outline' {
  if (rank === 1) return 'default';
  if (rank === 2) return 'secondary';
  return 'outline';
}

/**
 * Get rank icon based on rank
 */
export function getRankIcon(rank: number) {
  if (rank === 1) {
    return <LucideTrendingUp className="size-3 mr-1" />;
  }
  if (rank === 2) {
    return <LucideArrowUp className="size-3 mr-1" />;
  }
  if (rank === 3) {
    return <LucideArrowDown className="size-3 mr-1" />;
  }
  return null;
}

/**
 * Get sort icon based on sort state
 */
export function getSortIcon(isSorted: string | false) {
  if (isSorted === 'asc') {
    return <LucideArrowUp className="size-3 ml-1" />;
  }
  if (isSorted === 'desc') {
    return <LucideArrowDown className="size-3 ml-1" />;
  }
  return null;
}

/**
 * Create a fuzzy search function for filtering rows
 */
export function createFuzzySearchFn<T>(searchKeys: string[]) {
  return (row: Row<T>, columnId: string, filterValue: string): boolean => {
    const searchValue = filterValue.toLowerCase();
    return searchKeys.some((key) => {
      const value = getNestedValue(row.original, key);
      return String(value).toLowerCase().includes(searchValue);
    });
  };
}

function getNestedValue(obj: unknown, path: string): unknown {
  const keys = path.split('.');
  let result: unknown = obj;
  for (const key of keys) {
    if (result && typeof result === 'object' && key in result) {
      result = (result as Record<string, unknown>)[key];
    } else {
      return undefined;
    }
  }
  return result;
}

// Empty data constant for table initialization
export const EMPTY_DATA: unknown[] = [];
