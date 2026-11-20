import dayjs from 'dayjs';

/**
 * Format number with K/M suffix.
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
 * Format an exact count with thousands separators.
 */
export function formatCount(num: number): string {
  return (num ?? 0).toLocaleString();
}

/**
 * The API returns success_rate as a 0-1 fraction.
 */
export function formatSuccessRate(rate: number): string {
  return `${(Number(rate ?? 0) * 100).toFixed(2)}%`;
}

export function formatStatDate(
  statDate: string,
  granularity: AdminServiceEe.TokenStatsGranularity,
): string {
  const date = dayjs(statDate);
  if (!date.isValid()) {
    return statDate;
  }
  switch (granularity) {
    case 'hour':
      return date.format('YYYY-MM-DD HH:00');
    case 'month':
      return date.format('YYYY-MM');
    default:
      return date.format('YYYY-MM-DD');
  }
}
