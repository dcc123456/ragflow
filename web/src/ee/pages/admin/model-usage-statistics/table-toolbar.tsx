import { useTranslation } from 'react-i18next';

import type { DateRange } from '@/components/originui/calendar';

import { UsageDateRangePicker } from './date-range-picker';

interface ModelUsageToolbarProps {
  range: DateRange;
  onRangeChange: (range: DateRange) => void;
}

export function ModelUsageToolbar({
  range,
  onRangeChange,
}: ModelUsageToolbarProps) {
  const { t } = useTranslation();

  return (
    <div className="flex flex-row items-start justify-between gap-4">
      <div className="min-w-0">
        <div className="truncate text-2xl font-medium text-text-primary">
          {t('admin.modelUsage.modelUsageStatistics')}
        </div>
        <p className="mt-0.5 text-xs text-text-secondary">
          {t('admin.modelUsage.modelUsageSubtitle')}
        </p>
      </div>

      <div className="shrink-0">
        <UsageDateRangePicker range={range} onRangeChange={onRangeChange} />
      </div>
    </div>
  );
}
