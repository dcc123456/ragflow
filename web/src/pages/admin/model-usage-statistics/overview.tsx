// OverView component for displaying statistics cards
// Filters data based on filters through backend API

import type { FilterValues } from './types';

import { getLlmTraceSummary } from '@/services/admin-service';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { useCreateFilters } from './hooks';
import { StatCard } from './stat-card';
import { formatNumber } from './utils';

interface OverViewProps {
  filters: FilterValues;
}

const OverView = ({ filters }: OverViewProps) => {
  const { t } = useTranslation();
  const param = useCreateFilters(filters);
  // Fetch statistics with filters from backend
  const { data: statistics } = useQuery({
    queryKey: ['admin/usageStatistics', filters],
    queryFn: async () => {
      const res = await getLlmTraceSummary(param);
      return res.data.data || {};
    },
    initialData: {
      total_tokens: 0,
      input_tokens: 0,
      output_tokens: 0,
      total_requests: 0,
      avg_latency: 0,
    },
  });

  return (
    <div className="grid grid-cols-5 gap-4 mx-6 mb-6">
      <StatCard
        title={t('admin.modelUsage.totalTokens')}
        value={formatNumber(statistics?.total_tokens ?? 0)}
        // subtitle={t('admin.modelUsage.allTime')}
      />
      <StatCard
        title={t('admin.modelUsage.inputTokens')}
        value={formatNumber(statistics?.input_tokens ?? 0)}
        // subtitle={t('admin.modelUsage.prompts')}
      />
      <StatCard
        title={t('admin.modelUsage.outputTokens')}
        value={formatNumber(statistics?.output_tokens ?? 0)}
        // subtitle={t('admin.modelUsage.responses')}
      />
      <StatCard
        title={t('admin.modelUsage.totalRequests')}
        value={formatNumber(statistics?.total_requests ?? 0)}
        // subtitle={t('admin.modelUsage.apiCalls')}
      />
      <StatCard
        title={t('admin.modelUsage.avgLatency')}
        value={`${statistics?.avg_duration_ms ?? 0}ms`}
        // subtitle={t('admin.modelUsage.avgResponse')}
      />
    </div>
  );
};

export { OverView };
