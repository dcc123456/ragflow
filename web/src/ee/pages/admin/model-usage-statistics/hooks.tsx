import { useQuery } from '@tanstack/react-query';
import dayjs from 'dayjs';

import type { DateRange } from '@/components/originui/calendar';
import {
  getTokenStatsDetail,
  getTokenStatsSummary,
  getTokenStatsUsers,
} from '@/ee/services/admin-service';

import type { TokenStatsGranularity, TokenStatsWindow } from './types';

export const TokenStatsDateFormat = 'YYYY-MM-DD HH:mm:ss';

export function toTokenStatsWindow(range: DateRange): TokenStatsWindow {
  const to = range.to ?? range.from;

  return {
    from: dayjs(range.from).format(TokenStatsDateFormat),
    to: dayjs(to).format(TokenStatsDateFormat),
  };
}

export const TokenStatsKeys = {
  summary: (window: TokenStatsWindow) =>
    ['admin/tokenStats/summary', window.from, window.to] as const,
  users: (window: TokenStatsWindow, top: number) =>
    ['admin/tokenStats/users', window.from, window.to, top] as const,
  detail: (
    window: TokenStatsWindow,
    email: string,
    granularity: TokenStatsGranularity,
  ) =>
    [
      'admin/tokenStats/detail',
      window.from,
      window.to,
      granularity,
      email,
    ] as const,
};

export function useTokenStatsSummary(window: TokenStatsWindow) {
  return useQuery({
    queryKey: TokenStatsKeys.summary(window),
    queryFn: async () => {
      const response = await getTokenStatsSummary({
        from: window.from,
        to: window.to,
      });
      return response.data.data;
    },
    retry: false,
  });
}

export function useTokenStatsUsers(window: TokenStatsWindow, top: number) {
  return useQuery({
    queryKey: TokenStatsKeys.users(window, top),
    queryFn: async () => {
      const response = await getTokenStatsUsers({
        from: window.from,
        to: window.to,
        top,
      });
      return response.data.data ?? [];
    },
    retry: false,
  });
}

export function useTokenStatsDetail(
  window: TokenStatsWindow,
  email: string,
  granularity: TokenStatsGranularity,
) {
  return useQuery({
    queryKey: TokenStatsKeys.detail(window, email, granularity),
    queryFn: async () => {
      const response = await getTokenStatsDetail({
        from: window.from,
        to: window.to,
        granularity,
        user_name: email,
      });
      return response.data.data ?? [];
    },
    enabled: !!email,
    retry: false,
  });
}
