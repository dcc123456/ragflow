// Model Usage Statistics - Main Page Component
// Combines all sub-components for the complete page

import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Spotlight from '@/components/spotlight';
import { ScrollArea } from '@/components/ui/scroll-area';

import { useHandleExport } from './hooks';
import { OverView } from './overview';
import { RankingTable } from './ranking-table';
import { RequestLogs } from './request-logs';
import { TableToolbar } from './table-toolbar';
import type { FilterValues } from './types';
export default function ModelUsageStatistics() {
  const { t } = useTranslation();
  // Unified filters state management for view, timeRange, and searchValue
  const [filters, setFilters] = useState<FilterValues>({
    view: 'users',
    timeRange: '7days',
    searchValue: '',
  });
  const { handleExport } = useHandleExport(filters, 'ranking');

  // Unified handler for filter changes
  const handleFilterChange = useCallback((newFilters: FilterValues) => {
    setFilters(newFilters);
  }, []);

  return (
    <div className="relative h-full bg-transparent rounded-xl overflow-auto border-0.5 border-border-button">
      <Spotlight />

      <ScrollArea className="size-full">
        <div className="flex justify-between items-center">
          {/* Toolbar with search, view selector, time range, and export */}
          <div className="px-6 w-full py-5">
            <TableToolbar
              filters={filters}
              title={t('admin.modelUsage.modelUsageStatistics')}
              subtitle={t('admin.modelUsage.modelUsageSubtitle')}
              onChange={handleFilterChange}
              onExport={() => handleExport()}
              showViewSelector
            />
          </div>
        </div>

        {/* Overview Statistics Cards */}
        <OverView filters={filters} />

        {/* Ranking Table */}
        <div className="max-w-[calc(100vw-350px)] overflow-auto">
          <RankingTable
            filters={filters}
            // onExport={(data) => handleExport(data, 'ranking')}
          />
        </div>
        <div className="h-16 w-full" />

        {/* Request Logs */}
        <div className="max-w-[calc(100vw-350px)] overflow-auto">
          <RequestLogs />
        </div>
      </ScrollArea>
    </div>
  );
}
