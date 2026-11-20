import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';

import { SelectWithSearch } from '@/components/originui/select-with-search';
import { TruncatedText } from '@/components/truncated-text';
import { TableEmpty } from '@/components/table-skeleton';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import { Segmented, SegmentedValue } from '@/components/ui/segmented';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useClientPagination } from '@/hooks/logic-hooks/use-pagination';

import { useTokenStatsDetail } from './hooks';
import type {
  TokenStatsGranularity,
  TokenStatsUserItem,
  TokenStatsWindow,
} from './types';
import { formatCount, formatStatDate, formatSuccessRate } from './utils';

const GranularityOptions: TokenStatsGranularity[] = ['hour', 'day', 'month'];

interface UsageDetailProps {
  window: TokenStatsWindow;
  users: TokenStatsUserItem[];
  email: string;
  onEmailChange: (email: string) => void;
  granularity: TokenStatsGranularity;
  onGranularityChange: (granularity: TokenStatsGranularity) => void;
}

export function UsageDetail({
  window,
  users,
  email,
  onEmailChange,
  granularity,
  onGranularityChange,
}: UsageDetailProps) {
  const { t } = useTranslation();
  const { data, isLoading } = useTokenStatsDetail(window, email, granularity);
  const items = data ?? [];
  const { page, pageSize, setPage, onPaginationChange, pagedList } =
    useClientPagination(items);

  useEffect(() => {
    setPage(1);
  }, [email, granularity, window.from, window.to, setPage]);

  const handleGranularityChange = (value: SegmentedValue) => {
    onGranularityChange(value as TokenStatsGranularity);
  };

  return (
    <section>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-base font-medium text-text-primary">
          {t('admin.modelUsage.usageDetail')}
        </h2>

        <div className="flex items-center gap-3">
          <SelectWithSearch
            value={email}
            options={users.map((user) => ({
              value: user.email,
              label: user.email,
            }))}
            onChange={onEmailChange}
            placeholder={t('admin.modelUsage.selectUser')}
            emptyData={t('admin.modelUsage.selectUserHint')}
            triggerClassName="w-[240px]"
          />

          <Segmented
            options={GranularityOptions.map((option) => ({
              value: option,
              label: t(`admin.modelUsage.${option}`),
            }))}
            value={granularity}
            onChange={handleGranularityChange}
            sizeType="sm"
          />
        </div>
      </div>

      <Table className="table-fixed">
        <colgroup>
          <col className="w-[14%]" />
          <col className="w-[14%]" />
          <col className="w-[11%]" />
          <col className="w-[12%]" />
          <col className="w-[19%]" />
          <col className="w-[15%]" />
          <col className="w-[15%]" />
        </colgroup>
        <TableHeader>
          <TableRow>
            <TableHead>{t('admin.modelUsage.statDate')}</TableHead>
            <TableHead>{t('admin.modelUsage.tokens')}</TableHead>
            <TableHead>{t('admin.modelUsage.requests')}</TableHead>
            <TableHead>{t('admin.modelUsage.successRate')}</TableHead>
            <TableHead>{t('admin.modelUsage.modelName')}</TableHead>
            <TableHead>{t('admin.modelUsage.instanceName')}</TableHead>
            <TableHead>{t('admin.modelUsage.providerName')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {!email ? (
            <TableRow>
              <TableCell
                colSpan={7}
                className="h-24 text-center text-text-secondary"
              >
                {t('admin.modelUsage.selectUserHint')}
              </TableCell>
            </TableRow>
          ) : isLoading ? (
            <TableRow>
              <TableCell
                colSpan={7}
                className="h-24 text-center text-text-secondary"
              >
                {t('common.loading')}
              </TableCell>
            </TableRow>
          ) : pagedList.length ? (
            pagedList.map((item) => (
              <TableRow
                key={`${item.stat_date}-${item.model_name}-${item.provider_name}`}
                className="group"
              >
                <TableCell className="text-text-secondary group-hover:text-text-primary">
                  {formatStatDate(item.stat_date, granularity)}
                </TableCell>
                <TableCell className="text-text-secondary group-hover:text-text-primary">
                  {formatCount(item.total_tokens)}
                </TableCell>
                <TableCell className="text-text-secondary group-hover:text-text-primary">
                  {formatCount(item.request_count)}
                </TableCell>
                <TableCell className="text-text-secondary group-hover:text-text-primary">
                  {formatSuccessRate(item.success_rate)}
                </TableCell>
                <TableCell className="text-text-secondary group-hover:text-text-primary">
                  <TruncatedText className="truncate" tooltip={item.model_name}>
                    {item.model_name}
                  </TruncatedText>
                </TableCell>
                <TableCell className="text-text-secondary group-hover:text-text-primary">
                  {item.instance_name ? (
                    <TruncatedText
                      className="truncate"
                      tooltip={item.instance_name}
                    >
                      {item.instance_name}
                    </TruncatedText>
                  ) : (
                    '-'
                  )}
                </TableCell>
                <TableCell className="text-text-secondary group-hover:text-text-primary">
                  <TruncatedText
                    className="truncate"
                    tooltip={item.provider_name}
                  >
                    {item.provider_name}
                  </TruncatedText>
                </TableCell>
              </TableRow>
            ))
          ) : (
            <TableEmpty columnsLength={7} />
          )}
        </TableBody>
      </Table>

      {items.length > 0 && (
        <div className="flex items-center justify-end pt-4">
          <RAGFlowPagination
            total={items.length}
            current={page}
            pageSize={pageSize}
            onChange={onPaginationChange}
          />
        </div>
      )}
    </section>
  );
}
