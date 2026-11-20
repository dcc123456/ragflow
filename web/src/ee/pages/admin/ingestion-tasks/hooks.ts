import {
  keepPreviousData,
  useInfiniteQuery,
  useQuery,
} from '@tanstack/react-query';
import { useMemo } from 'react';

import type { IngestionMessageParams } from '@/interfaces/database/ingestion';
import {
  getIngestionLog,
  listIngestionLogMessages,
  listIngestionTasks,
} from '@/ee/services/admin-service';

const MessagesPageSize = 200;
const PollIntervalMs = 5000;
const ActiveLogStatuses = new Set(['0', '1', '5']);

export const IngestionTaskKeys = {
  all: () => ['admin', 'ingestionTasks'] as const,
  list: (params: AdminServiceEe.IngestionTaskListParams) =>
    [...IngestionTaskKeys.all(), params] as const,
  log: (logId: string) => ['admin', 'ingestionLog', logId] as const,
  messages: (logId: string) =>
    ['admin', 'ingestionLogMessages', logId] as const,
};

export function useIngestionTasks(
  params: AdminServiceEe.IngestionTaskListParams,
) {
  return useQuery({
    queryKey: IngestionTaskKeys.list(params),
    queryFn: async () => {
      const { data } = await listIngestionTasks(params);
      if (data.code !== 0) {
        throw new Error(data.message);
      }
      return { items: data.data ?? [], total: data.total ?? 0 };
    },
    placeholderData: keepPreviousData,
    retry: false,
  });
}

export function useIngestionLog(logId: string, enabled: boolean) {
  return useQuery({
    queryKey: IngestionTaskKeys.log(logId),
    queryFn: async () => {
      const { data } = await getIngestionLog(logId);
      if (data.code !== 0) {
        throw new Error(data.message);
      }
      return data.data;
    },
    enabled: enabled && !!logId,
    refetchInterval: (query) =>
      ActiveLogStatuses.has(query.state.data?.operation_status ?? '')
        ? PollIntervalMs
        : false,
    retry: false,
  });
}

export function useIngestionLogMessages(logId: string, enabled: boolean) {
  const query = useInfiniteQuery({
    queryKey: IngestionTaskKeys.messages(logId),
    enabled: enabled && !!logId,
    initialPageParam: undefined as number | undefined,
    queryFn: async ({ pageParam }) => {
      const params: IngestionMessageParams = { limit: MessagesPageSize };
      if (pageParam) {
        params.before_id = pageParam;
      }
      const { data } = await listIngestionLogMessages(logId, params);
      if (data.code !== 0) {
        throw new Error(data.message);
      }
      return data.data;
    },
    getNextPageParam: (lastPage) =>
      lastPage.has_more_before && lastPage.oldest_id
        ? lastPage.oldest_id
        : undefined,
    // The first page always holds the newest events, so polling it while the
    // run is active keeps the log tail live.
    refetchInterval: (query) =>
      query.state.data?.pages[0]?.terminal === false ? PollIntervalMs : false,
    retry: false,
  });

  const pages = query.data?.pages;
  const items = useMemo(
    () =>
      pages
        ? Array.from(
            new Map(
              pages
                .flatMap((page) => page.items)
                .map((item) => [item.id, item]),
            ).values(),
          ).sort((left, right) => left.id - right.id)
        : [],
    [pages],
  );
  const terminal = pages?.[0]?.terminal ?? false;
  const data = pages?.length
    ? {
        run_count: pages[0].run_count,
        items,
        oldest_id: items[0]?.id,
        newest_id: items[items.length - 1]?.id,
        has_more_before: query.hasNextPage,
        has_more_after: false,
        terminal,
      }
    : undefined;

  return { ...query, data };
}
