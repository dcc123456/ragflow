import { useTranslation } from 'react-i18next';

import { StatCard } from './stat-card';
import { useTokenStatsSummary } from './hooks';
import type { TokenStatsWindow } from './types';
import { formatCount, formatNumber, formatSuccessRate } from './utils';

interface OverViewProps {
  window: TokenStatsWindow;
}

export function OverView({ window }: OverViewProps) {
  const { t } = useTranslation();
  const { data: summary, isLoading } = useTokenStatsSummary(window);

  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
      <StatCard
        title={t('admin.modelUsage.totalTokens')}
        value={isLoading ? '-' : formatNumber(summary?.total_tokens ?? 0)}
      />
      <StatCard
        title={t('admin.modelUsage.totalRequests')}
        value={isLoading ? '-' : formatCount(summary?.request_count ?? 0)}
      />
      <StatCard
        title={t('admin.modelUsage.successRate')}
        value={isLoading ? '-' : formatSuccessRate(summary?.success_rate ?? 0)}
      />
    </div>
  );
}
