// Hooks for Model Usage Statistics

import { useEffect, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

import {
  createColumnHelper,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table';

import { useQuery } from '@tanstack/react-query';

import { Badge } from '@/components/ui/badge';
import { getLlmTraceByOrg, getLlmTraceRecent } from '@/services/admin-service';

import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import message from '@/components/ui/message';
import { useGetPaginationWithRouter } from '@/hooks/logic-hooks';
import { useDebounce } from 'ahooks';
import type {
  FilterValues,
  LlmTraceByOrgItem,
  LlmTraceRecent,
  ViewType,
} from './types';
import { createFuzzySearchFn, formatNumber } from './utils';

const columnHelper = createColumnHelper<LlmTraceByOrgItem>();
const columnTraceHelper = createColumnHelper<LlmTraceRecent>();

function getDimension(view: ViewType): 'user' | 'team' | 'dept' {
  switch (view) {
    case 'users':
      return 'user';
    case 'groups':
      return 'team';
    case 'departments':
      return 'dept';
    default:
      return 'user';
  }
}

function timeRangeToHours(timeRange: string): string {
  const mapping: Record<string, string> = {
    '1hour': '1',
    '6hours': '6',
    '24hours': '24',
    '7days': '168',
    '30days': '720',
    '90days': '2160',
    '1year': '8760',
  };
  return mapping[timeRange] || '168';
}

export function useCreateFilters(filters: FilterValues) {
  const dimension = getDimension(filters.view);
  const hours = timeRangeToHours(filters.timeRange);
  const debouncedSearchString = useDebounce(filters.searchValue, { wait: 500 });

  return {
    hours,
    dimension,
    keyword: debouncedSearchString,
  };
}

export function useRankingTableData(filters: FilterValues) {
  const param = useCreateFilters(filters);
  const { pagination, setPagination } = useGetPaginationWithRouter();
  const { data, isLoading } = useQuery({
    queryKey: ['admin/llm-trace/by-org', param, pagination],
    queryFn: async () => {
      const resp = await getLlmTraceByOrg({
        ...param,
        page: pagination.current,
        page_size: pagination.pageSize,
      });
      setPagination({
        ...pagination,
        total: resp.data.data.total,
        page: pagination.current,
      });
      return resp.data.data || [];
    },
    retry: false,
  });

  useEffect(() => {
    setPagination({
      page: 1,
    });
  }, [filters, setPagination]);

  return {
    data: data ?? [],
    isLoading,
    pagination,
    setPagination,
  };
}

export function useRankingTableColumns(view: ViewType) {
  const { t } = useTranslation();

  const columnDefs = useMemo(
    () => [
      columnHelper.accessor('rank', {
        header: 'rank',
        cell: ({ cell }) => cell.getValue(),
      }),
      ...(view === 'users'
        ? [
            columnHelper.display({
              id: 'email',
              header: t('admin.modelUsage.email'),
              cell: ({ row }) => row.original.info?.email ?? '-',
            }),
            columnHelper.display({
              id: 'nickname',
              header: t('admin.modelUsage.nickname', 'Nickname'),
              cell: ({ row }) => row.original.info?.nickname ?? '-',
            }),
            columnHelper.display({
              id: 'department',
              header: t('admin.modelUsage.department', 'Department'),
              cell: ({ row }) => (
                <div className="flex items-center gap-1">
                  {row.original.dept_info && row.original.dept_info?.length > 0
                    ? row.original.dept_info?.map((item) => {
                        return (
                          <div key={item.name}>
                            {item?.name ? (
                              <div className="flex items-center gap-1">
                                <RAGFlowAvatar
                                  className="size-5"
                                  isPerson
                                  avatar={item.avatar}
                                  name={item.name}
                                />
                                {item?.name}
                              </div>
                            ) : (
                              '-'
                            )}
                          </div>
                        );
                      })
                    : '-'}
                </div>
              ),
            }),
            columnHelper.display({
              id: 'group',
              header: t('admin.modelUsage.group', 'Group'),
              cell: ({ row }) => (
                <div className="flex items-center gap-1">
                  {row.original.team_info && row.original.team_info?.length > 0
                    ? row.original.team_info?.map((item) => {
                        return (
                          <div key={item.name}>
                            {item?.name ? (
                              <div className="flex items-center gap-1">
                                <RAGFlowAvatar
                                  className="size-5"
                                  isPerson
                                  avatar={item.avatar}
                                  name={item.name}
                                />
                                {item?.name}
                              </div>
                            ) : (
                              '-'
                            )}
                          </div>
                        );
                      })
                    : '-'}
                </div>
              ),
            }),
          ]
        : [
            columnHelper.display({
              id: 'name',
              header:
                view === 'departments'
                  ? t('admin.modelUsage.department')
                  : t('admin.modelUsage.group'),
              cell: ({ row }) => row.original.info?.name ?? '-',
            }),
          ]),
      columnHelper.display({
        id: 'input_tokens',
        header: t('admin.modelUsage.inputTokens'),
        cell: ({ row }) => formatNumber(row.original.input_tokens),
      }),
      columnHelper.display({
        id: 'output_tokens',
        header: t('admin.modelUsage.outputTokens'),
        cell: ({ row }) => formatNumber(row.original.output_tokens),
      }),
      columnHelper.display({
        id: 'total_tokens',
        header: t('admin.modelUsage.totalTokens'),
        cell: ({ row }) => <div>{formatNumber(row.original.total_tokens)}</div>,
      }),
      columnHelper.display({
        id: 'max_tokens',
        header: t('admin.modelUsage.maxTokens', 'Max Tokens'),
        cell: ({ row }) => formatNumber(row.original.max_tokens),
      }),
      columnHelper.display({
        id: 'avg_tokens_per_request',
        header: t('admin.modelUsage.avgTokensPerRequest', 'Avg Tokens/Req'),
        cell: ({ row }) => formatNumber(row.original.avg_tokens_per_request),
      }),
      columnHelper.display({
        id: 'request_count',
        header: t('admin.modelUsage.requests'),
        cell: ({ row }) => formatNumber(row.original.request_count),
      }),
      columnHelper.display({
        id: 'avg_duration_ms',
        header: t('admin.modelUsage.avgLatency'),
        cell: ({ row }) => `${row.original.avg_duration_ms}ms`,
      }),
      // ...(view === 'departments' || view === 'groups'
      //   ? [
      //       columnHelper.display({
      //         id: 'user_count',
      //         header: t('admin.modelUsage.userCount'),
      //         cell: ({ row }) =>
      //           row.original.user_count?.toLocaleString() ?? '-',
      //       }),
      //     ]
      //   : []),
    ],
    [t, view],
  );

  return columnDefs;
}

export function useRankingTable(filters: FilterValues) {
  const { data, isLoading, pagination, setPagination } =
    useRankingTableData(filters);
  const columnDefs = useRankingTableColumns(filters.view);

  const globalFilterFn = useMemo(
    () =>
      createFuzzySearchFn<LlmTraceByOrgItem>([
        'info.email',
        'info.name',
        'info.nickname',
      ]),
    [],
  );

  const table = useReactTable({
    data,
    columns: columnDefs,
    globalFilterFn,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    state: {
      globalFilter: filters.searchValue,
    },
    autoResetAll: false,
  });

  return { table, isLoading, pagination, setPagination };
}

export function useRequestLogsTable(filters: FilterValues) {
  const { t } = useTranslation();
  const param = useCreateFilters(filters);
  const { pagination, setPagination } = useGetPaginationWithRouter();

  const { data, isLoading } = useQuery({
    queryKey: ['admin/llm-trace/recent', param],
    queryFn: async () => {
      const resp = await getLlmTraceRecent(param);
      // setPagination({
      //   ...pagination,
      //   total: resp.data.data.total,
      //   page: pagination.current,
      // });
      return resp.data.data;
    },
    retry: false,
  });

  const columnDefs = useMemo(
    () => [
      columnTraceHelper.accessor('timestamp', {
        header: t('admin.modelUsage.date', 'Date'),
      }),
      columnTraceHelper.display({
        id: 'email',
        header: t('admin.modelUsage.email'),
        cell: ({ row }) => row.original.user_info?.email ?? '-',
      }),
      columnTraceHelper.display({
        id: 'nickname',
        header: t('admin.modelUsage.nickname', 'Nickname'),
        cell: ({ row }) => row.original.user_info?.nickname ?? '-',
      }),
      columnTraceHelper.display({
        id: 'department',
        header: t('admin.modelUsage.department', 'Department'),
        cell: ({ row }) => (
          <div className="flex items-center gap-1">
            {row.original.dept_info && row.original.dept_info?.length > 0
              ? row.original.dept_info?.map((item) => {
                  return (
                    <div key={item.name}>
                      {item?.name ? (
                        <div className="flex items-center gap-1">
                          <RAGFlowAvatar
                            isPerson
                            className="size-5"
                            avatar={item.avatar}
                            name={item.name}
                          />
                          {item?.name}
                        </div>
                      ) : (
                        '-'
                      )}
                    </div>
                  );
                })
              : '-'}
          </div>
        ),
      }),
      columnTraceHelper.display({
        id: 'group',
        header: t('admin.modelUsage.group', 'Group'),
        cell: ({ row }) => (
          <div className="flex items-center gap-1">
            {row.original.team_info && row.original.team_info?.length > 0
              ? row.original.team_info?.map((item) => {
                  return (
                    <div key={item.name}>
                      {item?.name ? (
                        <div className="flex items-center gap-1">
                          <RAGFlowAvatar
                            isPerson
                            className="size-5"
                            avatar={item.avatar}
                            name={item.name}
                          />
                          {item.name}
                        </div>
                      ) : (
                        '-'
                      )}
                    </div>
                  );
                })
              : '-'}
          </div>
        ),
      }),
      columnTraceHelper.display({
        id: 'model',
        header: t('admin.modelUsage.model', 'Model'),
        cell: ({ row }) => (
          <Badge variant="secondary" className="group-hover:text-text-primary">
            {row.original.model}
          </Badge>
        ),
      }),
      columnTraceHelper.display({
        id: 'duration_ms',
        header: t('admin.modelUsage.duration', 'Duration'),
        cell: ({ row }) => `${row.original.duration_ms}ms`,
      }),
      columnTraceHelper.display({
        id: 'input_tokens',
        header: t('admin.modelUsage.inputTokens'),
        cell: ({ row }) => formatNumber(row.original.input_tokens),
      }),
      columnTraceHelper.display({
        id: 'output_tokens',
        header: t('admin.modelUsage.outputTokens'),
        cell: ({ row }) => formatNumber(row.original.output_tokens),
      }),
      columnTraceHelper.display({
        id: 'total_tokens',
        header: t('admin.modelUsage.totalTokens'),
        cell: ({ row }) => formatNumber(row.original.total_tokens),
      }),
    ],
    [t],
  );

  const globalFilterFn = useMemo(
    () => createFuzzySearchFn<LlmTraceRecent>(['user_info.email']),
    [],
  );

  const table = useReactTable({
    data: data ?? [],
    columns: columnDefs,
    globalFilterFn,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    state: {
      globalFilter: filters.searchValue,
    },
  });

  return { table, isLoading, pagination, setPagination };
}

export const useHandleExport = (
  filter: FilterValues,
  type: 'ranking' | 'logs',
) => {
  const { t } = useTranslation();
  const param = useCreateFilters(filter);

  const handleExport = async () => {
    try {
      let resp;
      let exportData: LlmTraceByOrgItem[] = [];
      let headers: string[] = [];

      if (type === 'ranking') {
        resp = await getLlmTraceByOrg(param);
        exportData = resp?.data?.data || [];

        if (exportData.length === 0) {
          message.warning(t('common.noDataToExport'));
          return;
        }

        if (filter.view === 'users') {
          headers = [
            '#',
            t('admin.modelUsage.email'),
            t('admin.modelUsage.nickname', 'Nickname'),
            t('admin.modelUsage.department'),
            t('admin.modelUsage.group'),
            t('admin.modelUsage.inputTokens'),
            t('admin.modelUsage.outputTokens'),
            t('admin.modelUsage.totalTokens'),
            t('admin.modelUsage.maxTokens', 'Max Tokens'),
            t('admin.modelUsage.avgTokensPerRequest', 'Avg Tokens/Req'),
            t('admin.modelUsage.requests'),
            t('admin.modelUsage.avgLatency'),
          ];
        } else {
          headers = [
            '#',
            filter.view === 'departments'
              ? t('admin.modelUsage.department')
              : t('admin.modelUsage.group'),
            t('admin.modelUsage.inputTokens'),
            t('admin.modelUsage.outputTokens'),
            t('admin.modelUsage.totalTokens'),
            t('admin.modelUsage.maxTokens', 'Max Tokens'),
            t('admin.modelUsage.avgTokensPerRequest', 'Avg Tokens/Req'),
            t('admin.modelUsage.requests'),
            t('admin.modelUsage.avgLatency'),
            // t('admin.modelUsage.userCount'),
          ];
        }
      } else if (type === 'logs') {
        resp = await getLlmTraceRecent(param);
        const logsData: LlmTraceRecent[] = resp?.data?.data || [];

        if (logsData.length === 0) {
          message.warning(t('common.noDataToExport'));
          return;
        }

        headers = [
          t('admin.modelUsage.date'),
          t('admin.modelUsage.email'),
          t('admin.modelUsage.nickname', 'Nickname'),
          t('admin.modelUsage.department'),
          t('admin.modelUsage.team'),
          t('admin.modelUsage.model'),
          t('admin.modelUsage.duration', 'Duration'),
          t('admin.modelUsage.inputTokens'),
          t('admin.modelUsage.outputTokens'),
          t('admin.modelUsage.totalTokens'),
        ];

        const csvRows = [
          headers.join(','),
          ...logsData.map((item: LlmTraceRecent) => {
            const row = [
              item.timestamp || '',
              item.user_info?.email || '-',
              item.user_info?.nickname || '-',
              item.dept_info?.length > 0
                ? item.dept_info?.map((item) => item.name || '-').join(',')
                : '-',
              item.team_info?.length > 0
                ? item.team_info?.map((item) => item.name || '-').join(',')
                : '-',
              item.model,
              `${item.duration_ms}ms`,
              formatNumber(item.input_tokens),
              formatNumber(item.output_tokens),
              formatNumber(item.total_tokens),
            ];
            return row.map((cell) => `"${cell}"`).join(',');
          }),
        ];

        downloadCSV(
          csvRows.join('\n'),
          `model-usage-logs-${getDateString()}.csv`,
        );
        return;
      }

      const csvRows = [
        headers.join(','),
        ...exportData.map((item) => {
          let row: string[];

          if (filter.view === 'users') {
            row = [
              item.rank?.toString() || '',
              item.info?.email || '-',
              item.info?.nickname || '-',
              item.dept_info?.length > 0
                ? item.dept_info?.map((d) => d.name).join(',')
                : '-',
              item.team_info?.length > 0
                ? item.team_info?.map((t) => t.name).join(',')
                : '-',
              formatNumber(item.input_tokens),
              formatNumber(item.output_tokens),
              formatNumber(item.total_tokens),
              formatNumber(item.max_tokens),
              formatNumber(item.avg_tokens_per_request),
              formatNumber(item.request_count),
              `${item.avg_duration_ms}ms`,
            ];
          } else {
            row = [
              item.rank?.toString() || '',
              item.info?.name || '-',
              formatNumber(item.input_tokens),
              formatNumber(item.output_tokens),
              formatNumber(item.total_tokens),
              formatNumber(item.max_tokens),
              formatNumber(item.avg_tokens_per_request),
              formatNumber(item.request_count),
              `${item.avg_duration_ms}ms`,
              // item.user_count?.toLocaleString() ?? '-',
            ];
          }

          return row.map((cell) => `"${cell}"`).join(',');
        }),
      ];

      downloadCSV(
        csvRows.join('\n'),
        `model-usage-ranking-${getDateString()}.csv`,
      );
    } catch (error) {
      console.error('Export failed:', error);
      message.error(t('common.exportFailed'));
    }
  };

  return {
    handleExport,
  };
};

function getDateString(): string {
  return new Date().toISOString().split('T')[0];
}

function downloadCSV(csvContent: string, filename: string): void {
  const BOM = '\uFEFF';
  const blob = new Blob([BOM + csvContent], {
    type: 'text/csv;charset=utf-8;',
  });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
