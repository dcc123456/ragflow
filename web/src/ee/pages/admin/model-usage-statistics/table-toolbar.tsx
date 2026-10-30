// TableToolbar component for ranking-table and request-logs

import { useTranslation } from 'react-i18next';

import { LucideSearch, LucideUpload } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

import { SelectWithSearch } from '@/components/originui/select-with-search';
import type { FilterValues, ViewType } from './types';

// Static options to avoid re-creation on every render
const timeRangeOptions = [
  { value: '1hour', label: 'admin.modelUsage.last1Hour' },
  { value: '6hours', label: 'admin.modelUsage.last6Hours' },
  { value: '24hours', label: 'admin.modelUsage.last24Hours' },
  { value: '7days', label: 'admin.modelUsage.last7Days' },
  { value: '30days', label: 'admin.modelUsage.last30Days' },
  { value: '90days', label: 'admin.modelUsage.last90Days' },
  { value: '1year', label: 'admin.modelUsage.last1Year' },
];

const viewOptions = [
  { value: 'users', label: 'admin.modelUsage.users' },
  { value: 'departments', label: 'admin.modelUsage.departments' },
  { value: 'groups', label: 'admin.modelUsage.groups' },
];

export interface TableToolbarProps {
  filters: FilterValues;
  onChange: (filters: FilterValues) => void;
  onExport: () => void;
  showViewSelector?: boolean;
  title?: string;
  subtitle?: string;
}

export function TableToolbar({
  filters,
  onChange,
  onExport,
  showViewSelector = false,
  title,
  subtitle,
}: TableToolbarProps) {
  const { t } = useTranslation();

  // Unified handler for view change
  const handleViewChange = (newValue: ViewType) => {
    onChange({ ...filters, view: newValue });
  };

  // Unified handler for time range change
  const handleTimeRangeChange = (newValue: string) => {
    onChange({ ...filters, timeRange: newValue });
  };

  // Unified handler for search value change
  const handleSearchChange = (newValue: string) => {
    onChange({ ...filters, searchValue: newValue });
  };

  return (
    <div className="flex flex-row items-center justify-between space-y-0">
      {!!title && !!subtitle && (
        <div>
          <div className="text-2xl font-medium text-text-primary">{title}</div>
          {subtitle && (
            <p className="text-xs text-text-secondary mt-0.5">{subtitle}</p>
          )}
        </div>
      )}

      <div className="flex items-center gap-3">
        <div className="relative w-48">
          <LucideSearch className="absolute left-3 top-1/2 transform -translate-y-1/2 text-text-secondary size-4" />
          <Input
            className="pl-9 h-9 bg-bg-input border-border-button text-sm"
            placeholder={t('header.search')}
            value={filters.searchValue}
            onChange={(e) => handleSearchChange(e.target.value)}
          />
        </div>
        {showViewSelector && (
          <SelectWithSearch
            value={filters.view}
            onChange={handleViewChange}
            options={viewOptions.map((option) => ({
              ...option,
              label: t(option.label as keyof typeof t),
            }))}
            placeholder={t('admin.view')}
            triggerClassName="w-[130px]"
          />
        )}

        <SelectWithSearch
          value={filters.timeRange}
          onChange={handleTimeRangeChange}
          options={timeRangeOptions.map((option) => ({
            ...option,
            label: t(option.label as keyof typeof t),
          }))}
          placeholder={t('admin.selectTimeRange')}
          triggerClassName="w-[140px]"
        />

        <Button variant="default" size="sm" onClick={onExport}>
          <LucideUpload className="size-4 mr-2" />
          {t('admin.export')}
        </Button>
      </div>
    </div>
  );
}
